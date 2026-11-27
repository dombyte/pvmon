package collector_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/profiles"
)

// The simulation runs a virtual device through the real collector and storage on a fake
// clock and checks rules of spec 5.3 that hold for any data. TestSimulation exercises the
// core with a stress profile in every scenario; TestProfileSimulation (sim_profile_test.go)
// verifies device templates. PVMON_SIM_SEED changes the random data.

const (
	simInterval = 30 * time.Second
	simLead     = 2 * time.Hour // run before the first midnight
	simTail     = time.Hour     // run after the last midnight: its reset gets confirmed
)

// stressProfile has every counter feature of the core that built-in profiles lack.
const stressProfile = `schema: 1
profile: test/stress
version: 1
vendor: Test
transport: modbus
modbus: { function: input, byte_order: little, word_order: little }
read: { mode: range, max_registers: 60, ranges: [[100, 159]] }
max_power: 20000
blocks:
  pv:          # daily in 100 Wh steps without power: spread evenly
    role: pv
    channels:
      energy_out: { addr: 100, type: u16, scale: 0.1, unit: kWh, counter: daily_reset, reset_group: day, resolution: 0.1 }
  battery:     # daily in 1 Wh and 1 kWh steps, both directions, same group
    role: battery
    channels:
      power: { addr: 102, type: i32, scale: 1, unit: W, invert: true }
      energy_in: { addr: 104, type: u32, scale: 1, unit: Wh, counter: daily_reset, reset_group: day }
      energy_out: { addr: 106, type: u16, scale: 1, unit: kWh, counter: daily_reset, reset_group: day, resolution: 1 }
  backup:      # daily in 10 Wh steps
    role: backup
    channels:
      power: { addr: 108, type: i16, scale: 0.01, unit: kW }
      energy_in: { addr: 110, type: u32, scale: 0.01, unit: kWh, counter: daily_reset, reset_group: day }
  grid:        # lifetime u32 and f32
    role: grid
    channels:
      power: { addr: 112, type: i32, unit: W }
      energy_in: { addr: 114, type: u32, scale: 0.01, unit: kWh, counter: lifetime }
      energy_out: { addr: 116, type: f32, unit: kWh, counter: lifetime }
  wallbox:     # lifetime in 1 kWh steps
    role: consumer
    category: wallbox
    channels:
      power: { addr: 118, type: u32, unit: W }
      energy_in: { addr: 120, type: u32, scale: 1, unit: kWh, counter: lifetime }
  heater:      # power only: integrated
    role: consumer
    category: heatpump
    channels:
      power: { addr: 122, type: u32, unit: W }
`

// scenario is what happens to the installation.
type scenario struct {
	name   string
	start  time.Time // first local midnight
	drift  time.Duration
	netted bool
	down   [2]time.Duration // polls fail in this window after the first midnight
	every  time.Duration    // one poll fails every so often
	glitch time.Duration    // one poll reads nonsense counters then (after the first midnight)
	zero   time.Duration    // one poll reads all counters 0 then
	swap   time.Duration    // the lifetime meters are replaced then
	wrap   bool             // u32 lifetime counters overflow during the run
	night  bool             // a quiet evening and full power after the first midnight
	stops  []stop
	flush  int // every n-th flush fails
}

// stop is a pvmon stop (or crash) for some time.
type stop struct {
	at, down time.Duration // at: after the first midnight
	crash    bool
}

func scenarios() []scenario {
	july := time.Date(2026, 7, 10, 0, 0, 0, 0, berlin)
	h, m := time.Hour, time.Minute
	return []scenario{
		{name: "clean", start: july},
		{name: "netting app", start: july, netted: true},
		{name: "clock early", start: july, drift: -40 * time.Second},
		{name: "clock late", start: july, drift: 7 * m},
		{name: "DST to winter", start: time.Date(2026, 10, 25, 0, 0, 0, 0, berlin)},
		{name: "DST to summer", start: time.Date(2026, 3, 29, 0, 0, 0, 0, berlin)},
		{name: "failed polls", start: july, every: 47 * m},
		{name: "outage", start: july, down: [2]time.Duration{10 * h, 11 * h}},
		{name: "outage over midnight", start: july, down: [2]time.Duration{23 * h, 25 * h}},
		{name: "glitch", start: july, glitch: 9 * h},
		{name: "zero read", start: july, zero: 9 * h},
		{name: "meter replaced", start: july, swap: 9 * h},
		{name: "counter overflow", start: july, wrap: true},
		{name: "night charging", start: july, night: true},
		{name: "quick restart", start: july, stops: []stop{{at: 12 * h, down: m}}},
		{
			name: "restart over midnight", start: july,
			stops: []stop{{at: 23 * h, down: 3 * h}},
		},
		{name: "crash", start: july, stops: []stop{{at: 13*h + 7*m, down: m, crash: true}}},
		{name: "flush errors", start: july, flush: 3},
	}
}

// TestSimulation battle tests the core: every counter feature in every scenario.
func TestSimulation(t *testing.T) {
	t.Parallel()
	stress := inlineProfile(t, stressProfile)
	for _, d := range simDevices(stress) {
		for _, sc := range scenarios() {
			t.Run(sc.name, func(t *testing.T) {
				t.Parallel()
				newSim(t, d, sc).run(t).check(t)
			})
		}
	}
}

func builtinProfile(t *testing.T, id string) *profile.Loaded {
	t.Helper()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	p, err := l.Load(id, repoRoot)
	require.NoError(t, err)
	return p
}

// sim is one scenario of one profile.
type sim struct {
	sc    scenario
	e     *env
	site  *site.Site
	dev   *vdev
	first time.Time // first poll
	end   time.Time
}

func newSim(t *testing.T, d simDevice, sc scenario) *sim {
	t.Helper()
	first := sc.start.Add(-simLead)
	meters := map[string]site.MeterSpec{}
	for _, b := range d.blocks {
		m := site.MeterSpec{Device: "d1", Source: b}
		if of, ok := shareParent(d.profile.Blocks[b]); ok && of != b &&
			slices.Contains(d.blocks, of) {
			m.Parent = of
		}
		meters[b] = m
	}
	s := simSite(t, d.profile, sc, meters)
	e := newEnv(t, first)
	dev := newVdev(e.clock, s.Devices["d1"], simSeed())
	dev.drift, dev.netted = sc.drift, sc.netted && meters["refgrid"].Device == ""
	end := sc.start.AddDate(0, 0, 2).Add(simTail) // three resets, two full days
	run := &sim{sc: sc, e: e, site: s, dev: dev, first: first, end: end}
	run.faults()
	return run
}

func simSeed() uint64 {
	n, err := strconv.ParseUint(os.Getenv("PVMON_SIM_SEED"), 10, 64)
	if err != nil {
		return 1
	}
	return n
}

func (s *sim) faults() {
	sc := s.sc
	s.dev.down, s.dev.garble = sc.pollFails, sc.garble
	if sc.night {
		s.dev.night = sc.start
	}
	if sc.swap > 0 {
		s.dev.replace = sc.start.Add(sc.swap)
	}
	if sc.wrap {
		s.dev.wrapSoon()
	}
}

// pollFails reports whether the poll at now fails.
func (sc scenario) pollFails(now time.Time) bool {
	d := now.Sub(sc.start)
	if sc.every > 0 && d.Truncate(sc.every) == d {
		return true
	}
	return d >= sc.down[0] && d < sc.down[1]
}

// garble is what a counter register that holds v reads at now.
func (sc scenario) garble(now time.Time, v float64) float64 {
	switch {
	case sc.glitch > 0 && now.Equal(sc.start.Add(sc.glitch)):
		return v*3 + simStart
	case sc.zero > 0 && now.Equal(sc.start.Add(sc.zero)):
		return 0
	}
	return v
}

// flakyStore fails every n-th flush.
type flakyStore struct {
	storage.CollectorStore
	n     int
	count atomic.Int64
}

func (f *flakyStore) Flush(ctx context.Context, b storage.Batch) error {
	if f.n > 0 && f.count.Add(1)%int64(f.n) == 0 {
		return errors.New("disk full")
	}
	return f.CollectorStore.Flush(ctx, b)
}

// simSite is a site with the profile under test as d1. A profile without a grid block
// gets an unpolled second device for the grid meter that every site needs.
func simSite(t *testing.T, p *profile.Loaded, sc scenario, meters map[string]site.MeterSpec,
) *site.Site {
	t.Helper()
	netting := "meter"
	if sc.netted {
		netting = "app"
	}
	address := "tcp://10.0.0.30"
	if p.Transport == profile.TransportHTTP {
		address = "http://sim-device"
	}
	devs := map[string]site.DeviceSpec{"d1": {
		Address: address, Unit: 1, Profile: p, Interval: simInterval, Netting: netting,
	}}
	if !hasGrid(p.Profile, meters) {
		devs["d2"] = site.DeviceSpec{
			Address: "http://sim-grid", Profile: builtinProfile(t, "shelly/pro-3em"),
			Interval: simInterval,
		}
		meters["refgrid"] = site.MeterSpec{Device: "d2", Source: "total"}
	}
	s, err := site.Build(site.Spec{Devices: devs, Meters: meters})
	require.NoError(t, err)
	return s
}

func hasGrid(p *profile.Profile, meters map[string]site.MeterSpec) bool {
	for _, m := range meters {
		if p.Blocks[m.Source].Role == profile.RoleGrid {
			return true
		}
	}
	return false
}

// build is the collector of the device under test.
func (s *sim) build(t *testing.T, st storage.CollectorStore) *collector.Collector {
	t.Helper()
	c, err := collector.New(collector.Settings{
		Site: s.site, Connection: s.site.Devices["d1"].Connection, Location: berlin,
		Sources: map[string]collector.Source{"d1": s.dev},
	}, s.e.deps(t, st, quietReporter(t)))
	require.NoError(t, err)
	return c
}

func (s *sim) run(t *testing.T) *sim {
	t.Helper()
	st := &flakyStore{CollectorStore: s.e.store.Collector(), n: s.sc.flush}
	session := func(until time.Time, crash bool) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c := s.build(t, st)
		require.NoError(t, c.Start(ctx))
		s.e.runUntil(until, simInterval)
		if crash {
			cancel() // the loop ends without the final flush
			s.e.clock.BlockUntil(0)
			return
		}
		st.n = 0 // the final flush succeeds
		require.NoError(t, c.Stop())
		st.n = s.sc.flush
	}
	for _, x := range s.sc.stops {
		session(s.sc.start.Add(x.at), x.crash)
		s.e.clock.Advance(x.down)
	}
	session(s.end, false)
	return s
}

// disturbed reports whether pvmon missed polls for longer than a slow cycle near slot.
func (s *sim) disturbed(slot int64) bool {
	near := func(from, to time.Duration) bool {
		return slot >= collector.Slot(s.sc.start.Add(from))-1 &&
			slot <= collector.Slot(s.sc.start.Add(to))+1
	}
	if s.sc.down[1] > 0 && near(s.sc.down[0], s.sc.down[1]) {
		return true
	}
	for _, x := range s.sc.stops {
		if x.down > collector.GapAfter && near(x.at, x.at+x.down) {
			return true
		}
	}
	return false
}

// afterOutage reports whether an outage started before slot.
func (s *sim) afterOutage(slot int64) bool {
	after := func(d time.Duration) bool { return slot >= collector.Slot(s.sc.start.Add(d))-1 }
	for _, x := range s.sc.stops {
		if x.down > collector.GapAfter && after(x.at) {
			return true
		}
	}
	return s.sc.down[1] > 0 && after(s.sc.down[0])
}

// interrupted reports whether any poll is missed.
func (s *sim) interrupted() bool {
	return s.sc.every > 0 || s.sc.down[1] > 0 || len(s.sc.stops) > 0
}

// series is one checked energy series.
type series struct {
	maxPower   float64 // W the device can put into the series
	key        string
	m          *meterSim // nil: integrated or shared
	integrated bool
	shared     bool
	shaped     bool // power places the energy in its slots
	netted     bool
	rows       map[int64]storage.EnergyBucket
}

func (s *sim) series(t *testing.T) []series {
	t.Helper()
	var out []series
	for _, key := range slices.Sorted(mapKeys(s.dev.truth)) {
		meter, channel, _ := cut(key)
		blk := s.dev.dev.Profile.Blocks[meter]
		c, declared := blk.Channels[channel]
		sr := series{
			key: key, integrated: !declared, shared: c.Derive != "", maxPower: s.dev.limit,
		}
		_, sr.shaped = blk.Channels["power"]
		sr.netted = s.dev.netted && blk.Role == profile.RoleGrid
		for _, m := range s.dev.meters {
			if m.block+"/"+m.channel == key {
				sr.m = m
			}
		}
		if declared && !c.Historised(channel) {
			continue
		}
		sr.rows = s.rows(t, meter, channel)
		out = append(out, sr)
	}
	return out
}

func (s *sim) rows(t *testing.T, meter, channel string) map[int64]storage.EnergyBucket {
	t.Helper()
	ctx := context.Background()
	id, err := s.e.store.Read().SeriesID(ctx,
		storage.SeriesKey{Meter: meter, Channel: channel})
	if err != nil {
		return nil // not historised (phase energy)
	}
	b, err := s.e.store.Read().EnergyBuckets(ctx, id, collector.Slot(s.first),
		collector.Slot(s.end)+1)
	require.NoError(t, err)
	out := map[int64]storage.EnergyBucket{}
	for _, x := range b {
		out[x.Slot] = x
	}
	return out
}

// tolerance is what a series may differ from the truth at one edge: one register step and
// one poll of power (a counter shows whole steps, a poll sees one instant).
func (sr series) tolerance() float64 {
	tol := sr.maxPower * simInterval.Hours()
	if sr.m != nil {
		tol += sr.m.step
	}
	return tol
}

func (s *sim) check(t *testing.T) {
	t.Helper()
	all := s.series(t)
	require.NotEmpty(t, all)
	for _, sr := range all {
		require.NotEmpty(t, sr.rows, "%s: nothing booked", sr.key)
		s.checkSlots(t, sr)
		if sr.m != nil && sr.m.daily() {
			s.checkDays(t, sr)
			continue
		}
		s.checkTotal(t, sr)
	}
}

// checkSlots: no negative energy, flags only where spec 5.3 sets them, no missing slot and
// every slot of a series with power near the truth.
func (s *sim) checkSlots(t *testing.T, sr series) {
	t.Helper()
	var bad problems
	// The first slot is partial and the last one still open.
	for slot := collector.Slot(s.first) + 1; slot < collector.Slot(s.end)-1; slot++ {
		if s.disturbed(slot) {
			if b, ok := sr.rows[slot]; ok {
				bad.addf(b.Wh < -1e-6, "slot %d negative", slot)
			}
			continue
		}
		s.checkSlot(&bad, sr, slot)
	}
	bad.report(t, sr.key+": slots")
}

// checkSlot checks one slot pvmon watched without an outage.
func (s *sim) checkSlot(bad *problems, sr series, slot int64) {
	at := collector.SlotStart(slot).In(berlin).Format("01-02 15:04")
	b, ok := sr.rows[slot]
	if !ok {
		bad.addf(!s.late(sr, slot), "%s missing", at)
		return
	}
	bad.addf(b.Wh < -1e-6, "%s negative: %.1f Wh", at, b.Wh)
	s.checkFlags(bad, sr, slot, at)
	// A share splits a delta by the power of one poll (spec 4.2): an estimate. An
	// integrator misses the energy of a gap.
	if s.mayGap(sr) || sr.shared || !sr.shaped || s.late(sr, slot) {
		return
	}
	want := s.dev.truth[sr.key][slot]
	bad.addf(math.Abs(b.Wh-want) > 2*sr.tolerance(), "%s booked %.0f Wh, truth %.0f",
		at, b.Wh, want)
}

// checkFlags: gap and estimated only where spec 5.3 sets them.
func (s *sim) checkFlags(bad *problems, sr series, slot int64, at string) {
	flags := sr.rows[slot].Flags
	// An integrator cannot bridge a missed poll: that span is a gap.
	bad.addf(!s.mayGap(sr) && flags&storage.FlagGap != 0, "%s flagged gap", at)
	// A delta that covers an outage may be booked hours later (an idle counter), and
	// flags every slot from the outage start to then.
	mayEstimate := sr.integrated || sr.shared || s.afterOutage(slot)
	bad.addf(!mayEstimate && flags&storage.FlagEstimated != 0, "%s flagged estimated", at)
}

func (s *sim) mayGap(sr series) bool { return sr.integrated && s.interrupted() }

// late reports slots a counter cannot have booked yet: since its last step.
func (s *sim) late(sr series, slot int64) bool {
	if sr.m == nil || sr.m.step < 100 {
		return false
	}
	// A coarse counter books a slot only at its next step: check up to 2 h before the end.
	return collector.SlotStart(slot).After(s.end.Add(-2 * time.Hour))
}

// checkDays: a device day holds what its register showed just before the reset.
func (s *sim) checkDays(t *testing.T, sr series) {
	t.Helper()
	days := s.dev.closed[sr.key]
	require.GreaterOrEqual(t, len(days), 3, "resets of %s", sr.key)
	for i, d := range days[1:] { // the first day started before pvmon
		day := d.midnight.AddDate(0, 0, -1)
		from, to := collector.Slot(day), collector.Slot(d.midnight)
		if s.disturbed(from) || s.disturbed(to) {
			continue
		}
		got := 0.0
		for slot := from; slot < to; slot++ {
			got += sr.rows[slot].Wh
		}
		// One poll of power may fall on either side of each reset.
		tol := (s.resetPower(sr, days[i])+s.resetPower(sr, d))*simInterval.Hours() + 1
		if s.sc.drift != 0 {
			tol += sr.maxPower*s.sc.drift.Abs().Hours() + sr.m.step
		}
		assert.InDelta(t, d.register, got, tol, "%s: day %s", sr.key, day.Format("01-02"))
	}
}

// resetPower bounds the power that may be booked across a reset. The first slot of the
// booking counts in proportion to the time (spec 5.3), so its average power applies; a
// series without power is spread evenly, which any power of the device can fill.
func (s *sim) resetPower(sr series, d closedDay) float64 {
	if !sr.shaped {
		return sr.maxPower
	}
	before := s.dev.truth[sr.key][collector.Slot(d.midnight)-1]
	return max(d.power, before*float64(time.Hour/(storage.SlotSeconds*time.Second)))
}

// checkTotal: a lifetime, netted, shared or integrated series loses and invents nothing.
func (s *sim) checkTotal(t *testing.T, sr series) {
	t.Helper()
	if sr.integrated && s.interrupted() || sr.shared {
		return // a gap stays a gap; shares are estimates
	}
	from, to := collector.Slot(s.first)+1, collector.Slot(s.end.Add(-2*time.Hour))
	got, want := 0.0, 0.0
	for slot := from; slot < to; slot++ {
		got += sr.rows[slot].Wh
		want += s.dev.truth[sr.key][slot]
	}
	tol := s.totalTolerance(sr, want)
	assert.InDelta(t, want, got, tol, "%s: total", sr.key)
}

// totalTolerance: both ends of a total cut through a step.
func (s *sim) totalTolerance(sr series, want float64) float64 {
	tol := 2 * sr.tolerance()
	if s.sc.zero > 0 {
		// A clear decrease is a new meter (spec 5.3): the reading that drops, the held
		// one and the one that confirms it book nothing.
		tol += 3 * sr.maxPower * simInterval.Hours()
	}
	if sr.integrated || sr.netted {
		tol += 0.002 * want // trapezoids of single polls
	}
	return tol
}

// problems collects violations and reports the first few of them.
type problems []string

func (p *problems) addf(cond bool, format string, args ...any) {
	if cond {
		*p = append(*p, fmt.Sprintf(format, args...))
	}
}

func (p problems) report(t *testing.T, what string) {
	t.Helper()
	const show = 4
	if len(p) > 0 {
		t.Errorf("%s: %d violations, first: %s", what, len(p),
			strings.Join(p[:min(show, len(p))], "; "))
	}
}
