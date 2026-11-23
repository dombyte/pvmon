package app

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/configstore"
	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/transport/modbus"
	"github.com/dombyte/pvmon/internal/util"
)

// probeTimeout bounds one setup probe, all requests of the device included.
const probeTimeout = 15 * time.Second

// ProbeFactory builds a one-shot source for device d of a draft site. bus is the transport
// of the collector running on d's connection, nil when none runs; release ends whatever the
// source opened itself.
type ProbeFactory func(s *site.Site, d *site.Device, bus collector.Transport, clock util.Clock,
	log zerolog.Logger) (src collector.Source, release func(), err error)

// RealProbe reads through the running collector's Modbus connection, so a probe never opens
// a second client on the same bus (AGENTS.md section 7). Only an address no collector
// serves yet gets a connection of its own for the probe; HTTP devices get a fresh client.
func RealProbe(s *site.Site, d *site.Device, bus collector.Transport, clock util.Clock,
	log zerolog.Logger,
) (collector.Source, func(), error) {
	conn := s.Connections[d.Connection]
	release := func() {}
	if !IsModbus(conn) {
		f, err := CreateFetcher(s, d, nil, clock, log)
		return f, release, err
	}
	mb, ok := bus.(*modbus.Conn)
	if !ok {
		var err error
		if mb, err = CreateModbusConn(conn, clock, log); err != nil {
			return nil, nil, err
		}
		mb.Start()
		release = mb.Stop
	}
	f, err := CreateFetcher(s, d, mb, clock, log)
	if err != nil {
		release()
		return nil, nil, err
	}
	return f, release, nil
}

// busRegistry remembers the transport of every running collector by connection address.
type busRegistry struct {
	mu sync.Mutex
	m  map[string]collector.Transport
}

func (b *busRegistry) set(addr string, tr collector.Transport) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[string]collector.Transport{}
	}
	b.m[addr] = tr
}

func (b *busRegistry) drop(addr string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.m, addr)
}

func (b *busRegistry) get(addr string) collector.Transport {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.m[addr]
}

// Probe implements handler.ConfigService: it applies the draft changes to a copy of the UI
// configuration, validates the whole, and reads every meter block of the device once.
// Probes run one at a time, so two of them never open the same new address twice.
func (r *runtime) Probe(ctx context.Context, by string, cs []configstore.Change, device string,
) (service.ProbeResult, error) {
	l, err := r.draft(by, cs)
	if err != nil {
		return service.ProbeResult{}, err
	}
	d, ok := l.Site.Devices[device]
	if !ok {
		return service.ProbeResult{}, configstore.ErrNotFound
	}
	all := *d
	all.Blocks = nil
	for name := range d.Profile.Blocks {
		all.Blocks = append(all.Blocks, name)
	}
	r.probeMu.Lock()
	defer r.probeMu.Unlock()
	log := r.log.With().Str("component", "probe").Str("device", device).Logger()
	src, release, err := r.o.Probe(l.Site, &all, r.buses.get(d.Connection), r.o.Clock, log)
	if err != nil {
		return service.ProbeResult{}, err
	}
	defer release()
	return r.readOnce(ctx, &all, src), nil
}

// draft is the configuration with the changes applied, validated but not stored.
func (r *runtime) draft(by string, cs []configstore.Change) (*Loaded, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next, err := r.withChanges(r.snap, cs, by)
	if err != nil {
		return nil, err
	}
	return r.validated(next)
}

// readOnce fetches and decodes one sample; a failed read is reported in the result.
func (r *runtime) readOnce(ctx context.Context, d *site.Device, src collector.Source,
) service.ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	start := r.o.Clock.Now()
	res := service.ProbeResult{
		Device: d.ID, Profile: d.Profile.ID, Verified: d.Profile.Verified, At: start,
	}
	sample, err := src.Fetch(ctx)
	if err == nil {
		raw, derr := sample.Raw()
		if derr == nil {
			res.Blocks = service.ProbeBlocks(d.Profile.Profile, &raw)
		}
		err = derr
	}
	res.TookMS = r.o.Clock.Now().Sub(start).Milliseconds()
	if err != nil {
		res.Error = err.Error()
		res.Blocks = service.ProbeBlocks(d.Profile.Profile, nil)
	}
	return res
}
