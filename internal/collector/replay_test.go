package collector_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/collector/mocks"
	"github.com/dombyte/pvmon/internal/database"
	"github.com/dombyte/pvmon/internal/eventbus"
	healthmocks "github.com/dombyte/pvmon/internal/health/mocks"
	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util/clocktest"
	"github.com/dombyte/pvmon/profiles"
)

var berlin = mustLoc("Europe/Berlin")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

const pollInterval = 5 * time.Second

// solisModel simulates an S6 hybrid: PV bell curve by day, 500 W load, a battery that
// charges at noon and discharges in the evening; energies kept exactly in Wh and shown
// in 0.1 kWh daily registers that reset at local midnight; the grid comes from the
// external meter's lifetime registers (0.01 kWh) as in the profile.
type solisModel struct {
	day                       int
	pv, bi, bo, gi, ge, house float64 // Wh today
	giTotal, geTotal          float64 // Wh lifetime
}

func (m *solisModel) powers(t time.Time) (pv, bat, grid, load float64) {
	l := t.In(berlin)
	h := float64(l.Hour()) + float64(l.Minute())/60 + float64(l.Second())/3600
	if h > 7 && h < 17 {
		pv = 6000 * math.Sin(math.Pi*(h-7)/10)
	}
	load = 500
	switch {
	case h >= 11 && h < 14:
		bat = -1500 // charging
	case h >= 19 && h < 23:
		bat = 800 // discharging
	}
	grid = load - pv - bat
	return pv, bat, grid, load
}

// advance integrates the energies over dt ending at t, resetting at local midnight.
func (m *solisModel) advance(t time.Time, dt time.Duration) {
	if d := t.In(berlin).YearDay(); d != m.day {
		*m = solisModel{day: d, giTotal: m.giTotal, geTotal: m.geTotal}
	}
	pv, bat, grid, load := m.powers(t)
	hrs := dt.Hours()
	m.pv += pv * hrs
	m.house += load * hrs
	switch {
	case bat > 0:
		m.bo += bat * hrs
	case bat < 0:
		m.bi += -bat * hrs
	}
	switch {
	case grid > 0:
		m.gi += grid * hrs
		m.giTotal += grid * hrs
	case grid < 0:
		m.ge += -grid * hrs
		m.geTotal += -grid * hrs
	}
}

// u32 splits a lifetime Wh reading into the two words of a 0.01 kWh register.
func u32(wh float64) (uint16, uint16) {
	v := uint32(math.Floor(wh / 10))
	return uint16(v >> 16), uint16(v)
}

func reg(wh float64) uint16 { return uint16(math.Floor(wh / 100)) }

func (m *solisModel) words(t time.Time) map[int]uint16 {
	pv, bat, grid, load := m.powers(t)
	dir := uint16(0)
	if bat > 0 {
		dir = 1
	}
	g := uint32(int32(-grid)) // device: + = export
	ih, il := u32(m.giTotal + 5_000_000)
	eh, el := u32(m.geTotal + 2_000_000)
	return map[int]uint16{
		33035: reg(m.pv), 33057: 0, 33058: uint16(pv), 33029: 0, 33030: 0,
		33163: reg(m.bi), 33167: reg(m.bo), 33149: 0, 33150: uint16(math.Abs(bat)),
		33135: dir, 33139: 50, 33133: 3000,
		33283: ih, 33284: il, 33285: eh, 33286: el, 33263: uint16(g >> 16), 33264: uint16(g),
		33596: reg(m.house), 33148: uint16(load), // the whole load on the backup output
		33095: 3, 33116: 0, 33117: 0, 33118: 0, 33119: 0, 33120: 0, 33121: 1,
		33145: 0, 33146: 0,
	}
}

// replaySource serves the model at the fake clock's time.
type replaySource struct {
	clock *clocktest.Clock
	model *solisModel
	last  time.Time
	down  func(time.Time) bool
}

func (s *replaySource) Fetch(context.Context) (profile.Sample, error) {
	now := s.clock.Now()
	if !s.last.IsZero() {
		s.model.advance(now, now.Sub(s.last))
	}
	s.last = now
	if s.down != nil && s.down(now) {
		return profile.Sample{At: now}, context.DeadlineExceeded
	}
	return profile.Sample{At: now, Words: s.model.words(now)}, nil
}

func solisSite(t *testing.T, netting string) *site.Site {
	t.Helper()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	p, err := l.Load("solis/s6-hybrid", ".")
	require.NoError(t, err)
	s, err := site.Build(site.Spec{
		Connections: map[string]site.ConnectionSpec{"lan": {
			Address: "tcp://10.0.0.20",
			Timeout: 2 * time.Second,
		}},
		Devices: map[string]site.DeviceSpec{"wr1": {
			Connection: "lan", Unit: 1, Profile: p,
			Interval: pollInterval, Netting: netting,
		}},
		Meters: map[string]site.MeterSpec{
			"pv": {Device: "wr1", Source: "pv"}, "battery": {Device: "wr1", Source: "battery"},
			"grid": {Device: "wr1", Source: "grid"}, "backup": {Device: "wr1", Source: "backup"},
		},
	})
	require.NoError(t, err)
	return s
}

type rig struct {
	clock *clocktest.Clock
	store *storage.Storage
	site  *site.Site
	src   *replaySource
}

func newRig(t *testing.T, netting string, start time.Time) *rig {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pvmon.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, database.Migrate(ctx, db, nil, start))
	st, err := storage.New(db, berlin)
	require.NoError(t, err)
	clock := clocktest.New(start)
	return &rig{
		clock: clock, store: st, site: solisSite(t, netting),
		src: &replaySource{clock: clock, model: &solisModel{}},
	}
}

func (r *rig) collector(t *testing.T) *collector.Collector {
	t.Helper()
	return r.collectorWith(t, r.src)
}

func (r *rig) collectorWith(t *testing.T, src collector.Source) *collector.Collector {
	t.Helper()
	bus := eventbus.New()
	cache, err := livecache.New(bus, r.clock)
	require.NoError(t, err)
	rep := healthmocks.NewMockReporter(t)
	rep.EXPECT().DevicePoll(mock.Anything, mock.Anything).Maybe()
	rep.EXPECT().DeviceState(mock.Anything, mock.Anything).Maybe()
	tr := mocks.NewMockTransport(t)
	tr.EXPECT().Start().Maybe()
	tr.EXPECT().Stop().Maybe()
	c, err := collector.New(collector.Settings{
		Site: r.site, Connection: "tcp://10.0.0.20:502", Location: berlin,
		Sources: map[string]collector.Source{"wr1": src},
	}, collector.Deps{
		Store: r.store.Collector(), Cache: cache, Bus: bus, Reporter: rep,
		Transport: tr, Clock: r.clock, Log: zerolog.Nop(),
	})
	require.NoError(t, err)
	return c
}

// runUntil advances the fake clock poll by poll until end.
func (r *rig) runUntil(end time.Time) {
	for r.clock.Now().Before(end) {
		r.clock.BlockUntil(1)
		r.clock.Advance(pollInterval)
	}
	r.clock.BlockUntil(1)
}

func (r *rig) dayWh(t *testing.T, meter, channel string, day time.Time) float64 {
	t.Helper()
	ctx := context.Background()
	id, err := r.store.Read().SeriesID(ctx, storage.SeriesKey{Meter: meter, Channel: channel})
	require.NoError(t, err)
	from := collector.Slot(day)
	tot, err := r.store.Read().SumEnergy(ctx, id, from, collector.Slot(day.AddDate(0, 0, 1)))
	require.NoError(t, err)
	return tot.Wh
}

// TestReplaySolisDay replays a whole local day around midnight and a crash: the booked
// day energy of every series equals the daily register at the end of the day, as stored
// by solis (Gate 1 with a synthetic recording).
func TestReplaySolisDay(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 11, 19, 0, 0, 0, 0, berlin)
	r := newRig(t, "meter", day.Add(-10*time.Minute))
	ctx, cancel := context.WithCancel(context.Background())
	c := r.collector(t)
	require.NoError(t, c.Start(ctx))
	r.runUntil(day.Add(13 * time.Hour))
	// crash: the loop ends without the final flush (no Stop)
	cancel()
	r.clock.BlockUntil(0)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	r.clock.Advance(3 * time.Minute) // restart a little later
	c2 := r.collector(t)
	require.NoError(t, c2.Start(ctx2))
	r.runUntil(day.AddDate(0, 0, 1).Add(-pollInterval))
	expect := map[string]float64{
		"pv/energy_out":      float64(reg(r.src.model.pv)) * 100,
		"battery/energy_in":  float64(reg(r.src.model.bi)) * 100,
		"battery/energy_out": float64(reg(r.src.model.bo)) * 100,
		"grid/energy_in":     r.src.model.gi, // 0.01 kWh lifetime registers
		"grid/energy_out":    r.src.model.ge,
		"backup/energy_in":   float64(reg(r.src.model.house)) * 100,
	}
	r.runUntil(day.AddDate(0, 0, 1).Add(15 * time.Minute))
	require.NoError(t, c2.Stop())
	for key, want := range expect {
		m, ch, _ := cut(key)
		assert.InDelta(t, want, r.dayWh(t, m, ch, day), 10, key) // ±0.01 kWh
	}
	assert.Greater(t, expect["pv/energy_out"], 20000.0, "the model produced a real day")
	assert.Greater(t, r.dayWh(t, "pv", "energy_out", day.AddDate(0, 0, 1)), -1.0)
}

func cut(key string) (string, string, bool) {
	for i := range key {
		if key[i] == '/' {
			return key[:i], key[i+1:], true
		}
	}
	return key, "", false
}

// TestReplayOutageAndDST runs the DST day (92 slots) with a 40-minute device outage: the
// counters cover the gap, so the day still matches; the gap buckets are flagged.
func TestReplayOutageAndDST(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 3, 29, 0, 0, 0, 0, berlin)
	r := newRig(t, "meter", day.Add(-time.Minute))
	outFrom, outTo := day.Add(12*time.Hour), day.Add(12*time.Hour+40*time.Minute)
	r.src.down = func(t time.Time) bool { return !t.Before(outFrom) && t.Before(outTo) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := r.collector(t)
	require.NoError(t, c.Start(ctx))
	r.runUntil(day.AddDate(0, 0, 1).Add(-pollInterval))
	pv := float64(reg(r.src.model.pv)) * 100
	r.runUntil(day.AddDate(0, 0, 1).Add(5 * time.Minute))
	require.NoError(t, c.Stop())
	require.NoError(t, c.Stop(), "idempotent")
	assert.InDelta(t, pv, r.dayWh(t, "pv", "energy_out", day), 10)

	id, err := r.store.Read().SeriesID(context.Background(),
		storage.SeriesKey{Meter: "pv", Channel: "energy_out"})
	require.NoError(t, err)
	tot, err := r.store.Read().SumEnergy(context.Background(), id, collector.Slot(day),
		collector.Slot(day.AddDate(0, 0, 1)))
	require.NoError(t, err)
	assert.Equal(t, storage.FlagEstimated, tot.Flags, "the counter covers the outage: estimated, not a gap")
	assert.Equal(t, int64(92), collector.Slot(day.AddDate(0, 0, 1))-collector.Slot(day))
}

// TestReplayNettingApp nets Solis grid values with method 2 (coarse 0.01 kWh counters):
// the netted import minus export equals the integrated grid power.
func TestReplayNettingApp(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 11, 19, 0, 0, 0, 0, berlin)
	r := newRig(t, "app", day.Add(-time.Minute))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := r.collector(t)
	require.NoError(t, c.Start(ctx))
	r.runUntil(day.AddDate(0, 0, 1).Add(-pollInterval))
	wantNet := r.src.model.gi - r.src.model.ge
	r.runUntil(day.AddDate(0, 0, 1).Add(time.Minute))
	require.NoError(t, c.Stop())
	gotNet := r.dayWh(t, "grid", "energy_in", day) - r.dayWh(t, "grid", "energy_out", day)
	assert.InDelta(t, wantNet, gotNet, 50, "trapezoid vs model: one poll at the edges")
	assert.InDelta(t, 0, r.dayWh(t, "grid", "grid_cancelled", day), 0, "method 2 has none")
}

// TestReplayResetSurvivesRestart checks that the learned reset instant is persisted with the
// counter states and restored by a restarted collector (its next flush keeps it).
func TestReplayResetSurvivesRestart(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 11, 19, 0, 0, 0, 0, berlin)
	r := newRig(t, "meter", day.Add(-3*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, r.collector(t).Start(ctx))
	r.runUntil(day.Add(time.Hour))
	cancel()
	r.clock.BlockUntil(0)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	c2 := r.collector(t)
	require.NoError(t, c2.Start(ctx2))
	r.runUntil(day.Add(2 * time.Hour))
	require.NoError(t, c2.Stop())
	ids := make([]int64, 0, 32)
	for id := int64(1); id <= 32; id++ {
		ids = append(ids, id)
	}
	st, err := r.store.Collector().CounterStates(context.Background(), ids)
	require.NoError(t, err)
	resets := 0
	for _, s := range st {
		if s.ResetAt != 0 {
			resets++
			assert.Equal(t, day.UnixMilli(), s.ResetAt)
		}
	}
	assert.Positive(t, resets)
}

// session runs a collector from now until end and stops it (flushing).
func (r *rig) session(t *testing.T, end time.Time) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := r.collector(t)
	require.NoError(t, c.Start(ctx))
	r.runUntil(end)
	require.NoError(t, c.Stop())
}

// idle lets the device run on while pvmon is stopped.
func (r *rig) idle(until time.Time) {
	for r.clock.Now().Before(until) {
		r.clock.Advance(time.Minute)
		now := r.clock.Now()
		r.src.model.advance(now, time.Minute)
		r.src.last = now
	}
}

// TestReplayRestartsOverMidnight: pvmon stops at 22:45, runs a few minutes at 07:30 (it
// sees the daily reset but stops before confirming it) and comes back at 08:30. The new
// day's energy is spread from the expected reset (midnight) as a gap, the rest of the old
// day stays open, and no slot of the night is booked as a measured 0.
func TestReplayRestartsOverMidnight(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 11, 19, 0, 0, 0, 0, berlin)
	at := func(h, m int) time.Time { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	r := newRig(t, "meter", at(-24, 0))
	r.idle(at(-2, 0)) // the registers hold a whole evening when pvmon starts
	r.session(t, at(-1, -15))
	r.idle(at(7, 30))
	r.session(t, at(7, 35))
	r.idle(at(8, 30))
	r.session(t, at(9, 30))

	ctx := context.Background()
	id, err := r.store.Read().SeriesID(ctx, storage.SeriesKey{Meter: "backup", Channel: "energy_in"})
	require.NoError(t, err)
	before, err := r.store.Read().EnergyBuckets(ctx, id, collector.Slot(at(-1, 0)),
		collector.Slot(day))
	require.NoError(t, err)
	assert.Empty(t, before, "the old day after the last poll stays open, not 0")
	night, err := r.store.Read().EnergyBuckets(ctx, id, collector.Slot(day),
		collector.Slot(at(7, 30)))
	require.NoError(t, err)
	assert.Len(t, night, 30, "every slot since midnight is booked")
	for _, b := range night {
		assert.Equal(t, storage.FlagEstimated, b.Flags&(storage.FlagEstimated|storage.FlagGap),
			"slot %d is an estimate, not a gap", b.Slot)
		assert.Positive(t, b.Wh, "slot %d carries part of the night", b.Slot)
	}
	assert.InDelta(t, float64(reg(r.src.model.house))*100, r.dayWh(t, "backup", "energy_in", day),
		100, "the new day books what its daily register shows")
}
