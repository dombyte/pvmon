//go:build simulation

package collector_test

import (
	"context"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

// A generic virtual device: it holds the physical truth (power and energy of every block)
// and shows it through the registers of any profile. Everything device specific comes from
// the profile, so the simulation exercises the core as a real device would.

const (
	simStep     = 5 * time.Second // resolution of the physical truth
	simRamp     = 5 * time.Minute // power changes linearly, so one poll hides little
	simMaxPower = 8000.0          // W per block, less for a profile with a low max_power
	simPerPhase = 300.0           // W counted in both directions by a non-netting meter
	simStart    = 1_000_000.0     // Wh a lifetime counter starts at
	simVoltage  = 400.0           // V of a product power channel
)

// wave is a random power: it holds a value, then ramps to the next one.
type wave struct {
	rng         *rand.Rand
	low, high   float64
	from, to    float64
	start, hold time.Time // ramp from start to start+simRamp, then hold until hold
}

func (w *wave) at(t time.Time) float64 {
	if !t.Before(w.hold) {
		next := 0.0
		if w.rng.IntN(4) > 0 { // idle in one of four phases
			next = math.Round(w.low + w.rng.Float64()*(w.high-w.low))
		}
		w.from, w.to, w.start = w.to, next, t
		w.hold = t.Add(simRamp + time.Duration(w.rng.IntN(40))*time.Minute)
	}
	f := min(1, t.Sub(w.start).Seconds()/simRamp.Seconds())
	return w.from + (w.to-w.from)*f
}

// meterSim is one energy counter of the device.
type meterSim struct {
	block, channel string
	ch             profile.Channel
	sign           float64 // power sign that flows into it
	step           float64 // Wh of one register step
	wh             float64 // true energy: of the device day, or since the counter started
}

// register is what the device shows, in Wh.
func (m *meterSim) register() float64 {
	v := math.Floor(m.wh/m.step+1e-6) * m.step
	if m.ch.Type == "u32" {
		wrap := (math.MaxUint32 + 1.0) * m.ch.ScaleOr1() * unitWh(m.ch.Unit)
		v = math.Mod(v, wrap)
	}
	return v
}

func (m *meterSim) daily() bool { return m.ch.Counter == profile.CounterDailyReset }

func unitWh(unit string) float64 {
	return map[string]float64{"Wh": 1, "kWh": 1e3, "MWh": 1e6}[unit]
}

// stepWh is the register step: the profile resolution or one raw count, 1 Wh for floats.
func stepWh(c profile.Channel) float64 {
	raw := c.ScaleOr1() * unitWh(c.Unit)
	if c.Type == "f32" || c.Type == "number" {
		raw = 1
	}
	return max(raw, c.Resolution*unitWh(c.Unit))
}

// inSign is the power sign that means energy_in for a role (spec 2).
func inSign(role string) float64 {
	switch role {
	case profile.RolePV, profile.RoleBattery, profile.RoleInverter:
		return -1
	}
	return 1
}

// closedDay is a device day of a daily counter.
type closedDay struct {
	midnight time.Time // the local midnight the reset belongs to
	register float64   // the last value a poll saw before the reset
	power    float64   // W into the counter at the reset
}

// vdev is the virtual device.
type vdev struct {
	clock   *clocktest.Clock
	dev     *site.Device
	blocks  []string // metered blocks, sorted
	waves   map[string]*wave
	meters  []*meterSim
	drift   time.Duration // device clock: + resets after midnight, - before
	netted  bool          // the grid is netted by pvmon; the meter counts per phase
	now     time.Time
	limit   float64                      // W per block
	truth   map[string]map[int64]float64 // "meter/channel" → slot → Wh
	closed  map[string][]closedDay
	shown   map[string]float64 // the value of every counter at the last successful poll
	down    func(time.Time) bool
	garble  func(time.Time, float64) float64 // what a counter register reads at t
	night   time.Time                        // quiet before this midnight, full power after
	replace time.Time                        // the lifetime meters are replaced then
}

func newVdev(clock *clocktest.Clock, d *site.Device, seed uint64) *vdev {
	v := &vdev{
		limit: powerLimit(d.Profile.Profile),
		clock: clock, dev: d, waves: map[string]*wave{}, now: clock.Now(),
		truth: map[string]map[int64]float64{}, closed: map[string][]closedDay{},
		shown:  map[string]float64{},
		blocks: slices.Sorted(slices.Values(d.Blocks)),
	}
	for i, b := range v.blocks {
		blk := d.Profile.Blocks[b]
		w := &wave{rng: rand.New(rand.NewPCG(seed, uint64(i))), high: v.limit}
		if of, ok := shareParent(blk); ok && of != b {
			// The parts add up to their parent, which has to stay below the limit.
			w.high /= float64(len(v.sharers(of)))
		}
		switch blk.Role {
		case profile.RoleGrid, profile.RoleBattery, profile.RoleInverter:
			w.low = -w.high
		}
		v.waves[b] = w
		v.addMeters(b, blk)
	}
	return v
}

// powerLimit keeps the simulated power well below the glitch guard of the profile.
func powerLimit(p *profile.Profile) float64 {
	const headroom = 0.4 // night charging and the per-phase share come on top
	if p.MaxPower == 0 {
		return simMaxPower
	}
	return min(simMaxPower, headroom*p.MaxPower)
}

func (v *vdev) addMeters(block string, blk profile.Block) {
	for _, name := range slices.Sorted(mapKeys(blk.Channels)) {
		c := blk.Channels[name]
		if c.Counter == profile.CounterNone {
			continue
		}
		sign := inSign(blk.Role)
		if strings.HasSuffix(name, "_out") {
			sign = -sign
		}
		m := &meterSim{block: block, channel: name, ch: c, sign: sign, step: stepWh(c)}
		if !m.daily() {
			m.wh = simStart
		}
		v.meters = append(v.meters, m)
	}
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// sharers are the blocks that split a counter of block by their power.
func (v *vdev) sharers(block string) []string {
	var out []string
	for _, b := range v.blocks {
		for _, c := range v.dev.Profile.Blocks[b].Channels {
			if of, _, _ := profile.ChannelRef(c.Of); c.Derive != "" && of == block {
				out = append(out, b)
			}
		}
	}
	return out
}

// powers returns the power of every block at t; a shared block is the sum of its parts.
func (v *vdev) powers(t time.Time) map[string]float64 {
	p := map[string]float64{}
	for _, b := range v.blocks {
		p[b] = v.waves[b].at(t)
		if !v.night.IsZero() {
			p[b] = v.nightPower(t, v.waves[b], p[b])
		}
	}
	for _, b := range v.blocks {
		if parts := v.sharers(b); len(parts) > 0 {
			p[b] = 0
			for _, s := range parts {
				p[b] += p[s]
			}
		}
	}
	return p
}

// nightPower is a quiet evening followed by full power right after midnight (charging on a
// night tariff), so the new day passes the old one before its reset is confirmed.
func (v *vdev) nightPower(t time.Time, w *wave, p float64) float64 {
	const charge = 30 * time.Minute
	switch {
	case t.Before(v.night):
		return p / 10
	case t.Before(v.night.Add(charge)):
		return w.high
	}
	return p
}

// advance lets the device run up to now in small steps.
func (v *vdev) advance(now time.Time) {
	for v.now.Before(now) {
		next := v.now.Add(simStep)
		p := v.powers(next)
		v.resetDaily(v.now, next, p)
		v.replaceMeters(v.now, next)
		for _, m := range v.meters {
			v.count(m, p[m.block], next)
		}
		v.integrateTruth(p, next)
		v.now = next
	}
}

// resetDaily resets the daily counters when the device clock passes its midnight.
func (v *vdev) resetDaily(from, to time.Time, p map[string]float64) {
	a, b := from.Add(-v.drift).In(berlin), to.Add(-v.drift).In(berlin)
	if a.Day() == b.Day() {
		return
	}
	midnight := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, berlin)
	for _, m := range v.meters {
		if m.daily() {
			key := m.block + "/" + m.channel
			v.closed[key] = append(v.closed[key], closedDay{
				midnight: midnight, register: v.shown[key], power: max(0, m.sign*p[m.block]),
			})
			m.wh = 0
		}
	}
}

// replaceMeters restarts the lifetime counters of a new meter at 0.
func (v *vdev) replaceMeters(from, to time.Time) {
	if !from.Before(v.replace) || to.Before(v.replace) {
		return
	}
	for _, m := range v.meters {
		if !m.daily() {
			m.wh = 0
		}
	}
}

// wrapSoon starts every u32 lifetime counter shortly before its overflow.
func (v *vdev) wrapSoon() {
	const before = 5000 // Wh
	for _, m := range v.meters {
		if !m.daily() && m.ch.Type == "u32" {
			m.wh = (math.MaxUint32+1.0)*m.ch.ScaleOr1()*unitWh(m.ch.Unit) - before
		}
	}
}

// count books one step of power into a counter and into the truth of its series.
func (v *vdev) count(m *meterSim, power float64, at time.Time) {
	wh := max(0, m.sign*power) * simStep.Hours()
	m.wh += wh
	if v.netted && v.dev.Profile.Blocks[m.block].Role == profile.RoleGrid {
		m.wh += simPerPhase * simStep.Hours()
	}
	v.record(m.block+"/"+m.channel, at, wh)
}

// integrateTruth records what blocks without counters and shared counters should book.
func (v *vdev) integrateTruth(p map[string]float64, at time.Time) {
	for _, b := range v.blocks {
		blk := v.dev.Profile.Blocks[b]
		for name, c := range blk.Channels {
			if c.Derive != "" {
				v.record(b+"/"+name, at, max(0, p[b])*simStep.Hours())
			}
		}
		if !hasEnergy(blk) {
			pos, neg := site.IntegratedChannels(blk.Role)
			v.record(b+"/"+pos, at, max(0, p[b])*simStep.Hours())
			v.record(b+"/"+neg, at, max(0, -p[b])*simStep.Hours())
		}
	}
}

func hasEnergy(b profile.Block) bool {
	for name, c := range b.Channels {
		if c.IsEnergy() || strings.HasPrefix(name, "energy_") {
			return true
		}
	}
	return false
}

func (v *vdev) record(key string, at time.Time, wh float64) {
	if strings.HasSuffix(key, "/") {
		return // the role has no series for this direction
	}
	if v.truth[key] == nil {
		v.truth[key] = map[int64]float64{}
	}
	// The step ending at at belongs to the slot it started in.
	v.truth[key][collector.Slot(at.Add(-simStep))] += wh
}

// values are the normalised channel values the registers show.
func (v *vdev) values() map[string]float64 {
	p, out := v.powers(v.now), map[string]float64{}
	for _, b := range v.blocks {
		c, ok := v.dev.Profile.Blocks[b].Channels["power"]
		switch {
		case !ok:
		case len(c.Product) == 2: // a string: voltage × current
			out[c.Product[0]], out[c.Product[1]] = simVoltage, p[b]/simVoltage
		default:
			out[b+".power"] = p[b]
		}
	}
	for _, m := range v.meters {
		out[m.block+"."+m.channel] = v.garble(v.now, m.register())
	}
	return out
}

// Fetch implements collector.Source.
func (v *vdev) Fetch(context.Context) (profile.Sample, error) {
	now := v.clock.Now()
	v.advance(now)
	if v.down != nil && v.down(now) {
		return profile.Sample{At: now}, context.DeadlineExceeded
	}
	for _, m := range v.meters {
		v.shown[m.block+"/"+m.channel] = m.register()
	}
	s, err := v.dev.Profile.Encode(v.values())
	s.At = now
	return s, err //nolint:wrapcheck // test source
}
