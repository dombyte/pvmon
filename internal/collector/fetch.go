// Package collector polls the devices of one connection, normalises their values, turns
// counters into energy deltas, nets grid energy and books everything into 15-minute
// buckets (spec 5, 6). One collector instance serves one connection.
package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/transport/modbus"
	"github.com/dombyte/pvmon/internal/util"
)

// ModbusReader reads registers over the shared connection (transport/modbus.Conn).
type ModbusReader interface {
	Read(ctx context.Context, unit uint8, fn modbus.Function, start, count uint16) ([]uint16, error)
}

// HTTPReader fetches a device path (transport/http.Client).
type HTTPReader interface {
	Get(ctx context.Context, path string) ([]byte, error)
}

// Fetcher reads the raw data of one device per poll: the Modbus requests or due HTTP
// endpoints its configured meters need, nothing else (keeps the bus load low). Slow
// endpoints (interval > 0) are cached between fetches.
type Fetcher struct {
	dev    *site.Device
	needs  profile.Needs
	clock  util.Clock
	modbus ModbusReader
	http   HTTPReader
	slow   map[string]slowEndpoint
}

type slowEndpoint struct {
	at   time.Time
	body json.RawMessage
}

// NewModbusFetcher builds a fetcher for a Modbus device.
func NewModbusFetcher(dev *site.Device, clock util.Clock, r ModbusReader) (*Fetcher, error) {
	err := util.RequireAll("fetcher", util.D("device", dev), util.D("clock", clock),
		util.D("reader", r))
	if err != nil {
		return nil, err
	}
	return &Fetcher{dev: dev, needs: dev.Profile.NeedsFor(dev.Blocks), clock: clock, modbus: r}, nil
}

// NewHTTPFetcher builds a fetcher for an HTTP device.
func NewHTTPFetcher(dev *site.Device, clock util.Clock, r HTTPReader) (*Fetcher, error) {
	err := util.RequireAll("fetcher", util.D("device", dev), util.D("clock", clock),
		util.D("reader", r))
	if err != nil {
		return nil, err
	}
	return &Fetcher{
		dev: dev, needs: dev.Profile.NeedsFor(dev.Blocks), clock: clock, http: r,
		slow: map[string]slowEndpoint{},
	}, nil
}

// Fetch performs one poll. The poll instant is captured once at its start. Any failed
// request fails the whole poll, so the counters see a gap instead of partial data.
func (f *Fetcher) Fetch(ctx context.Context) (profile.Sample, error) {
	return f.fetch(ctx, f.needs, false)
}

// FetchAll reads everything the profile describes, every HTTP endpoint regardless of its
// interval (dumps and fixtures).
func (f *Fetcher) FetchAll(ctx context.Context) (profile.Sample, error) {
	return f.fetch(ctx, nil, true)
}

func (f *Fetcher) fetch(ctx context.Context, n profile.Needs, all bool) (profile.Sample, error) {
	s := profile.Sample{At: f.clock.Now()}
	var err error
	if f.modbus != nil {
		s.Words, err = f.fetchModbus(ctx, n)
	} else {
		s.JSON, err = f.fetchHTTP(ctx, s.At, n, all)
	}
	return s, err
}

// Invalidate drops cached slow endpoints, e.g. after an ID/value length mismatch.
func (f *Fetcher) Invalidate() { clear(f.slow) }

func (f *Fetcher) fetchModbus(ctx context.Context, n profile.Needs) (map[int]uint16, error) {
	p := f.dev.Profile
	fn := modbus.FuncInput
	if p.Modbus.Function == "holding" {
		fn = modbus.FuncHolding
	}
	words := map[int]uint16{}
	for _, r := range p.Requests(f.dev.Read, n) {
		// unit (0..247) and ranges (0..65535) are validated by site and profile
		unit, start := uint8(f.dev.Unit), uint16(r.Start)
		got, err := f.modbus.Read(ctx, unit, fn, start, uint16(r.Count))
		if err != nil {
			return nil, fmt.Errorf("collector: %s read %d+%d: %w", f.dev.ID, r.Start, r.Count, err)
		}
		for i, w := range got {
			words[r.Start+i] = w
		}
	}
	return words, nil
}

func (f *Fetcher) fetchHTTP(ctx context.Context, now time.Time, n profile.Needs, all bool,
) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for _, e := range f.dev.Profile.Endpoints(n, func(profile.Endpoint) bool { return true }) {
		if !all && !f.due(e, now) {
			out[e.ID] = f.slow[e.ID].body
			continue
		}
		body, err := f.get(ctx, e.URL)
		if err != nil {
			return nil, err
		}
		out[e.ID] = body
		if e.Interval > 0 {
			f.slow[e.ID] = slowEndpoint{at: now, body: body}
		}
	}
	return out, nil
}

// due reports whether an endpoint must be fetched at now (slow endpoints by interval).
func (f *Fetcher) due(e profile.Endpoint, now time.Time) bool {
	last, ok := f.slow[e.ID]
	return e.Interval == 0 || !ok || now.Sub(last.at) >= time.Duration(e.Interval)
}

// ErrNotJSON is returned for a response body that is not valid JSON.
var ErrNotJSON = errors.New("collector: response is not JSON")

func (f *Fetcher) get(ctx context.Context, path string) (json.RawMessage, error) {
	body, err := f.http.Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("collector: %s GET %s: %w", f.dev.ID, path, err)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%w: %s GET %s", ErrNotJSON, f.dev.ID, path)
	}
	return body, nil
}
