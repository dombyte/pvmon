package simulate

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/util"
)

const (
	voltage = 230.0
	socFull = 100.0
	socInit = 50.0
	// batteryWhPerPercent models a 10 kWh battery.
	batteryWhPerPercent = 100.0
)

// Source simulates one device: every block of its profile follows the household model.
type Source struct {
	dev      *site.Device
	clock    util.Clock
	loc      *time.Location
	roles    map[string]string  // block → role
	cats     map[string]string  // block → category
	shares   map[string]float64 // block → share of its role's power on this device
	needs    profile.Needs      // blocks to encode; nil: all
	counters map[string]*counter
	last     time.Time
	soc      float64
}

// NewSource builds the simulated device. It encodes only the blocks its meters use (plus
// their dependencies), all blocks when it has no meter.
func NewSource(s *site.Site, d *site.Device, clock util.Clock, loc *time.Location) *Source {
	src := &Source{
		dev: d, clock: clock, loc: loc, roles: map[string]string{},
		cats: map[string]string{}, shares: map[string]float64{},
		counters: map[string]*counter{}, soc: socInit,
	}
	if len(d.Blocks) > 0 {
		src.needs = d.Profile.NeedsFor(d.Blocks)
	}
	for name, b := range d.Profile.Blocks {
		src.roles[name], src.cats[name] = b.Role, b.Category
	}
	for _, id := range d.Meters {
		m := s.Meters[id]
		src.roles[m.Source], src.cats[m.Source] = m.Role, m.Category
		if m.Role == profile.RoleConsumer {
			src.shares[m.Source] = 1 / float64(count(s.Meters, m.Role, m.Category, nil))
		}
	}
	src.splitDevices(s, d)
	return src
}

// splitDevices shares PV and battery power between the devices of a site by their meters
// (two inverters each produce their part). Within a device, the PV string blocks (power =
// V × I) a meter uses share its PV part; unused strings stay dark.
func (src *Source) splitDevices(s *site.Site, d *site.Device) {
	pv := fraction(s, d, profile.RolePV)
	var strs []string
	for name, b := range d.Profile.Blocks {
		switch {
		case b.Role == profile.RoleBattery:
			src.shares[name] = fraction(s, d, profile.RoleBattery)
		case b.Role != profile.RolePV:
		case len(b.Channels["power"].Product) == 0:
			src.shares[name] = pv
		default: // a string: dark unless a meter uses it
			src.shares[name] = 0
			if slices.Contains(d.Blocks, name) {
				strs = append(strs, name)
			}
		}
	}
	for _, name := range strs {
		src.shares[name] = pv / float64(len(strs))
	}
}

// fraction is the part of a role's power on device d: its top-level meters of the role
// over those of the site; 1 when the site has none (a device simulated alone).
func fraction(s *site.Site, d *site.Device, role string) float64 {
	all := count(s.Meters, role, "", nil)
	if all == 0 {
		return 1
	}
	return float64(count(s.Meters, role, "", d)) / float64(all)
}

// count counts the top-level meters of a role (and category, if set), on d if not nil.
func count(meters map[string]*site.Meter, role, category string, d *site.Device) int {
	n := 0
	for _, m := range meters {
		if matches(m, role, category, d) {
			n++
		}
	}
	return n
}

func matches(m *site.Meter, role, category string, d *site.Device) bool {
	return m.Role == role && !m.Nested() && (category == "" || m.Category == category) &&
		(d == nil || m.Device == d.ID)
}

// Fetch returns the encoded device data at the current time.
func (s *Source) Fetch(context.Context) (profile.Sample, error) {
	now := s.clock.Now()
	dt := time.Duration(0)
	if !s.last.IsZero() {
		dt = now.Sub(s.last)
	}
	s.last = now
	p := PowersAt(now.In(s.loc))
	s.soc = min(socFull, max(0, s.soc-p.Battery*dt.Hours()/batteryWhPerPercent))
	values := map[string]float64{}
	for block, b := range s.dev.Profile.Blocks {
		if s.needs != nil && s.needs[block] == nil {
			continue
		}
		power := powerFor(p, s.roles[block], s.cats[block])
		if share, ok := s.shares[block]; ok {
			power *= share
		}
		s.blockValues(values, blockPoll{block: block, b: b, power: power, now: now, dt: dt})
	}
	sample, err := s.dev.Profile.Encode(values)
	sample.At = now
	return sample, err //nolint:wrapcheck // encode cannot fail with finite numbers
}

// blockPoll is one block of one simulated poll.
type blockPoll struct {
	block string
	b     profile.Block
	power float64
	now   time.Time
	dt    time.Duration
}

func (s *Source) blockValues(values map[string]float64, bp blockPoll) {
	block, power, now, dt := bp.block, bp.power, bp.now, bp.dt
	for name, c := range bp.b.Channels {
		key := block + "." + name
		switch {
		case name == "power":
			setPower(values, key, c, power)
		case name == "soc":
			values[key] = s.soc
		case c.Counter != profile.CounterNone:
			wh := energyPower(s.roles[block], name, power) * dt.Hours()
			values[key] = s.count(key, c, wh, now)
		case strings.Contains(name, "voltage"):
			values[key] = voltage
		}
	}
}

// setPower sets a power channel, or its voltage and current for a product channel.
func setPower(values map[string]float64, key string, c profile.Channel, power float64) {
	if len(c.Product) == len([2]int{}) {
		values[c.Product[0]], values[c.Product[1]] = voltage, power/voltage
		return
	}
	values[key] = power
}

// energyPower returns the power that feeds an energy channel under the sign rules of
// spec 2: grid and consumers count positive power as energy_in, pv, battery and inverter
// count positive power as energy_out; the other channel gets the negative part.
func energyPower(role, channel string, power float64) float64 {
	inIsPositive := role != profile.RolePV && role != profile.RoleBattery &&
		role != profile.RoleInverter
	if (channel == "energy_in") == inIsPositive {
		return power
	}
	return -power
}

func (s *Source) count(key string, c profile.Channel, wh float64, now time.Time) float64 {
	cnt, ok := s.counters[key]
	if !ok {
		cnt = &counter{daily: c.Counter == profile.CounterDailyReset, day: now.In(s.loc).YearDay()}
		if !cnt.daily {
			cnt.wh = lifetimeWh
		}
		s.counters[key] = cnt
	}
	return cnt.add(wh, now.In(s.loc))
}

// Sources is an app.SourceFactory that simulates every device of a connection.
func Sources(loc *time.Location) func(*site.Site, *site.Connection, util.Clock, zerolog.Logger,
) (map[string]collector.Source, collector.Transport, error) {
	return func(s *site.Site, c *site.Connection, clock util.Clock, _ zerolog.Logger,
	) (map[string]collector.Source, collector.Transport, error) {
		out := map[string]collector.Source{}
		for _, id := range c.Devices {
			out[id] = NewSource(s, s.Devices[id], clock, loc)
		}
		return out, noTransport{}, nil
	}
}

// Probe is an app.ProbeFactory that simulates the probed device like a running one.
func Probe(loc *time.Location) func(*site.Site, *site.Device, collector.Transport, util.Clock,
	zerolog.Logger) (collector.Source, func(), error) {
	return func(s *site.Site, d *site.Device, _ collector.Transport, clock util.Clock,
		_ zerolog.Logger,
	) (collector.Source, func(), error) {
		return NewSource(s, d, clock, loc), func() {}, nil
	}
}

type noTransport struct{}

func (noTransport) Start() {}
func (noTransport) Stop()  {}
