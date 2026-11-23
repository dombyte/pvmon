package collector_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
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
	"github.com/dombyte/pvmon/internal/health"
	healthmocks "github.com/dombyte/pvmon/internal/health/mocks"
	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	storagemocks "github.com/dombyte/pvmon/internal/storage/mocks"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

const (
	moreConn    = "tcp://10.0.0.30:502"
	moreAddress = "tcp://10.0.0.30"
	profHead    = `schema: 1
profile: test/more
version: 1
vendor: Test
transport: modbus
modbus: { function: input, byte_order: big, word_order: big }
read: { mode: range, max_registers: 20, ranges: [[100, 115]] }
max_power: 20000
blocks:
`
	// integration profile: a main block with power only plus a power-only grid block.
	integBlocks = `  main:
    role: %s
    channels:
      power: { addr: 100, type: i16, unit: kW }
      voltage: { addr: 101, type: u16, scale: 0.1, unit: V, history: false }
  grid:
    role: grid
    channels:
      power: { addr: 104, type: i32, unit: W }
`
	// netting profile: a grid block with Wh counters of the given resolution.
	nettingBlocks = `  grid:
    role: grid
    channels:
      power: { addr: 104, type: i32, unit: W }
      energy_in: { addr: 100, type: u32, unit: Wh, counter: lifetime, resolution: %d }
      energy_out: { addr: 102, type: u32, unit: Wh, counter: lifetime, resolution: %d }
`
)

// fnSource serves words computed from the fake time.
type fnSource struct {
	clock *clocktest.Clock
	f     func(time.Time) (map[int]uint16, error)
	calls atomic.Int32
}

func (s *fnSource) Fetch(context.Context) (profile.Sample, error) {
	s.calls.Add(1)
	now := s.clock.Now()
	w, err := s.f(now)
	return profile.Sample{At: now, Words: w}, err
}

// pvShareSource adds the pv1/pv2 voltage and current registers to the Solis replay.
type pvShareSource struct{ *replaySource }

func (s pvShareSource) Fetch(ctx context.Context) (profile.Sample, error) {
	smp, err := s.replaySource.Fetch(ctx)
	if err == nil {
		// pv1: 300.0 V * 5.0 A = 1500 W; pv2: 300.0 V * 2.5 A = 750 W
		smp.Words[33049], smp.Words[33050] = 3000, 50
		smp.Words[33051], smp.Words[33052] = 3000, 25
	}
	return smp, err
}

func inlineProfile(t *testing.T, yaml string) *profile.Loaded {
	t.Helper()
	p, err := profile.Parse([]byte(yaml))
	require.NoError(t, err)
	return &profile.Loaded{Profile: p}
}

func buildSite(t *testing.T, p *profile.Loaded, devs map[string]site.DeviceSpec,
	meters map[string]site.MeterSpec,
) *site.Site {
	t.Helper()
	for id, d := range devs {
		d.Profile, d.Connection = p, "lan"
		if d.Interval == 0 {
			d.Interval = pollInterval
		}
		devs[id] = d
	}
	s, err := site.Build(site.Spec{
		Connections: map[string]site.ConnectionSpec{"lan": {
			Address: moreAddress,
			Timeout: 2 * time.Second,
		}},
		Devices: devs, Meters: meters,
	})
	require.NoError(t, err)
	return s
}

// oneDevice is a site with device d1 and the given meters of the inline profile.
func oneDevice(t *testing.T, yaml string, dev site.DeviceSpec, meters ...string) *site.Site {
	t.Helper()
	ms := map[string]site.MeterSpec{}
	for _, m := range meters {
		ms[m] = site.MeterSpec{Device: "d1", Source: m}
	}
	return buildSite(t, inlineProfile(t, yaml),
		map[string]site.DeviceSpec{"d1": dev}, ms)
}

type env struct {
	clock *clocktest.Clock
	store *storage.Storage
	cache *livecache.Cache
}

func newEnv(t *testing.T, start time.Time) *env {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir()+"/pvmon.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, database.Migrate(ctx, db, nil, start))
	st, err := storage.New(db, berlin)
	require.NoError(t, err)
	clock := clocktest.New(start)
	cache, err := livecache.New(eventbus.New(), clock)
	require.NoError(t, err)
	return &env{clock: clock, store: st, cache: cache}
}

func quietReporter(t *testing.T) *healthmocks.MockReporter {
	t.Helper()
	rep := healthmocks.NewMockReporter(t)
	rep.EXPECT().DevicePoll(mock.Anything, mock.Anything).Maybe()
	rep.EXPECT().DeviceState(mock.Anything, mock.Anything).Maybe()
	return rep
}

func (e *env) deps(t *testing.T, st storage.CollectorStore, rep health.Reporter) collector.Deps {
	t.Helper()
	tr := mocks.NewMockTransport(t)
	tr.EXPECT().Start().Maybe()
	tr.EXPECT().Stop().Maybe()
	return collector.Deps{
		Store: st, Cache: e.cache, Bus: eventbus.New(), Reporter: rep,
		Transport: tr, Clock: e.clock, Log: zerolog.Nop(),
	}
}

func (e *env) build(t *testing.T, s *site.Site, srcs map[string]collector.Source,
) *collector.Collector {
	t.Helper()
	return e.buildWith(t, e.store.Collector(), s, srcs)
}

func (e *env) buildWith(t *testing.T, st storage.CollectorStore, s *site.Site,
	srcs map[string]collector.Source,
) *collector.Collector {
	t.Helper()
	conn := ""
	for k := range s.Connections {
		conn = k
	}
	c, err := collector.New(collector.Settings{
		Site: s, Connection: conn, Location: berlin, Sources: srcs,
	}, e.deps(t, st, quietReporter(t)))
	require.NoError(t, err)
	return c
}

// runUntil advances the fake clock in steps until end.
func (e *env) runUntil(end time.Time, step time.Duration) {
	for e.clock.Now().Before(end) {
		e.clock.BlockUntil(1)
		e.clock.Advance(step)
	}
	e.clock.BlockUntil(1)
}

func (e *env) total(t *testing.T, meter, ch string, from, to time.Time) storage.EnergyTotal {
	t.Helper()
	ctx := context.Background()
	id, err := e.store.Read().SeriesID(ctx, storage.SeriesKey{Meter: meter, Channel: ch})
	require.NoError(t, err, meter+"/"+ch)
	tot, err := e.store.Read().SumEnergy(ctx, id, collector.Slot(from), collector.Slot(to))
	require.NoError(t, err)
	return tot
}

// runStopped starts c, runs the clock to end and stops c (flushing).
func (e *env) runStopped(t *testing.T, c *collector.Collector, end time.Time) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, c.Start(ctx))
	e.runUntil(end, pollInterval)
	require.NoError(t, c.Stop())
}

func i16w(v int) uint16 { return uint16(int16(v)) }

func i32w(v int) (uint16, uint16) {
	u := uint32(int32(v))
	return uint16(u >> 16), uint16(u)
}

var moreStart = time.Date(2026, 6, 10, 10, 0, 0, 0, berlin)

func TestShareSplitsPVByPower(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 6, 10, 6, 50, 0, 0, berlin)
	r := newRig(t, "meter", start)
	l := solisSite(t, "meter")
	dev := l.Devices["wr1"].Profile
	s := buildSite(t, dev, map[string]site.DeviceSpec{"wr1": {Unit: 1, Rollover: "23:59"}},
		map[string]site.MeterSpec{
			"pv1": {Device: "wr1", Source: "pv1"}, "pv2": {Device: "wr1", Source: "pv2"},
			"grid": {Device: "wr1", Source: "grid"},
		})
	e := &env{clock: r.clock, store: r.store}
	cache, err := livecache.New(eventbus.New(), r.clock)
	require.NoError(t, err)
	e.cache = cache
	c := e.build(t, s, map[string]collector.Source{"wr1": pvShareSource{r.src}})
	e.runStopped(t, c, start.Add(6*time.Hour))
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, berlin)
	end := day.AddDate(0, 0, 1)
	pv1 := e.total(t, "pv1", "energy_out", day, end)
	pv2 := e.total(t, "pv2", "energy_out", day, end)
	assert.InDelta(t, float64(reg(r.src.model.pv))*100, pv1.Wh+pv2.Wh, 10)
	assert.InDelta(t, 2.0, pv1.Wh/pv2.Wh, 0.01, "split follows the 1500:750 W ratio")
	assert.NotZero(t, pv1.Flags&storage.FlagEstimated, "shares are estimated")
}

func integProfile(role string) string { return profHead + fmt.Sprintf(integBlocks, role) }

func TestIntegratesPVPower(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, integProfile("pv"), site.DeviceSpec{Unit: 1, History: []string{"voltage"}},
		"main", "grid")
	src := &fnSource{clock: e.clock, f: func(time.Time) (map[int]uint16, error) {
		return map[int]uint16{100: 1, 101: 2300}, nil // 1 kW
	}}
	c := e.build(t, s, map[string]collector.Source{"d1": src})
	e.runStopped(t, c, moreStart.Add(time.Hour))
	tot := e.total(t, "main", "energy_out", moreStart.Add(-time.Hour), moreStart.Add(2*time.Hour))
	assert.InDelta(t, 1000, tot.Wh, 10, "trapezoid of 1000 W over one hour")
	assert.NotZero(t, tot.Flags&storage.FlagEstimated)
	v, ok := e.cache.Get("main/power")
	require.True(t, ok)
	assert.Equal(t, "W", v.Unit, "kW is published as W")
	assert.InDelta(t, 1000, v.Num, 0.001)
	_, err := e.store.Read().SeriesID(context.Background(),
		storage.SeriesKey{Meter: "main", Channel: "voltage"})
	assert.NoError(t, err, "history: [voltage] stores the gauge")
}

func TestIntegratesBatteryBothWaysWithMissingPower(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, integProfile("battery"), site.DeviceSpec{Unit: 1}, "main", "grid")
	src := &fnSource{clock: e.clock, f: func(now time.Time) (map[int]uint16, error) {
		switch d := now.Sub(moreStart); {
		case d < 30*time.Minute:
			return map[int]uint16{100: 1}, nil
		case d < 40*time.Minute:
			return map[int]uint16{}, nil // power missing: integrator restarts
		}
		return map[int]uint16{100: i16w(-1)}, nil
	}}
	c := e.build(t, s, map[string]collector.Source{"d1": src})
	e.runStopped(t, c, moreStart.Add(time.Hour))
	from, to := moreStart.Add(-time.Hour), moreStart.Add(2*time.Hour)
	assert.InDelta(t, 500, e.total(t, "main", "energy_out", from, to).Wh, 15)
	assert.InDelta(t, 333, e.total(t, "main", "energy_in", from, to).Wh, 15)
	gap := func(a, b time.Duration) int {
		return e.total(t, "main", "energy_out", moreStart.Add(a), moreStart.Add(b)).Flags &
			storage.FlagGap
	}
	assert.Zero(t, gap(0, 15*time.Minute), "the first poll of a run has no span to flag")
	assert.NotZero(t, gap(30*time.Minute, 45*time.Minute), "the span without power is missing")
	assert.Zero(t, gap(45*time.Minute, time.Hour), "integration resumes complete")
}

func nettingProfile(res int) string { return profHead + fmt.Sprintf(nettingBlocks, res, res) }

func gridWords(imp, exp uint32, power int) map[int]uint16 {
	ph, pl := i32w(power)
	return map[int]uint16{
		100: uint16(imp >> 16), 101: uint16(imp), 102: uint16(exp >> 16), 103: uint16(exp),
		104: ph, 105: pl,
	}
}

func TestNettingCountersCancelsWithinPoll(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, nettingProfile(1), site.DeviceSpec{Unit: 1, Netting: "app"}, "grid")
	var imp, exp uint32
	var wantIn, wantOut, wantCancel float64
	n := 0
	src := &fnSource{clock: e.clock, f: func(now time.Time) (map[int]uint16, error) {
		di, de := uint32(10), uint32(4)
		if now.Sub(moreStart) >= 30*time.Minute {
			di, de = 2, 7
		}
		imp, exp = imp+di, exp+de
		if n++; n > 1 { // the first poll only sets the base
			wantIn += max(float64(di)-float64(de), 0)
			wantOut += max(float64(de)-float64(di), 0)
			wantCancel += float64(min(di, de))
		}
		return gridWords(imp, exp, 0), nil
	}}
	c := e.build(t, s, map[string]collector.Source{"d1": src})
	e.runStopped(t, c, moreStart.Add(time.Hour))
	from, to := moreStart.Add(-time.Hour), moreStart.Add(2*time.Hour)
	assert.InDelta(t, wantIn, e.total(t, "grid", "energy_in", from, to).Wh, 3)
	assert.InDelta(t, wantOut, e.total(t, "grid", "energy_out", from, to).Wh, 3)
	assert.InDelta(t, wantCancel, e.total(t, "grid", "grid_cancelled", from, to).Wh, 3)
}

func TestNettingPowerSkipsMissingPower(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, nettingProfile(100), site.DeviceSpec{Unit: 1, Netting: "app"}, "grid")
	src := &fnSource{clock: e.clock, f: func(now time.Time) (map[int]uint16, error) {
		w := gridWords(0, 0, 1000)
		switch d := now.Sub(moreStart); {
		case d >= 20*time.Minute && d < 30*time.Minute:
			delete(w, 104)
			delete(w, 105)
		case d >= 30*time.Minute:
			w = gridWords(0, 0, -500)
		}
		return w, nil
	}}
	c := e.build(t, s, map[string]collector.Source{"d1": src})
	e.runStopped(t, c, moreStart.Add(50*time.Minute))
	from, to := moreStart.Add(-time.Hour), moreStart.Add(2*time.Hour)
	assert.InDelta(t, 333, e.total(t, "grid", "energy_in", from, to).Wh, 15)
	assert.InDelta(t, 167, e.total(t, "grid", "energy_out", from, to).Wh, 15)
}

func TestTwoDevicesOnOneConnection(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	p := inlineProfile(t, integProfile("pv"))
	s := buildSite(t, p, map[string]site.DeviceSpec{
		"d1": {Unit: 1, Interval: 5 * time.Second},
		"d2": {Unit: 2, Interval: 7 * time.Second},
	}, map[string]site.MeterSpec{
		"main": {Device: "d1", Source: "main"}, "grid": {Device: "d1", Source: "grid"},
		"pv2": {Device: "d2", Source: "main"},
	})
	f := func(time.Time) (map[int]uint16, error) { return map[int]uint16{100: 1}, nil }
	a, b := &fnSource{clock: e.clock, f: f}, &fnSource{clock: e.clock, f: f}
	c := e.build(t, s, map[string]collector.Source{"d1": a, "d2": b})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, c.Start(ctx))
	e.runUntil(moreStart.Add(70*time.Second), time.Second)
	require.NoError(t, c.Stop())
	assert.InDelta(t, 14, a.calls.Load(), 2)
	assert.InDelta(t, 10, b.calls.Load(), 2)
}

func TestNewRejectsBadInput(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, integProfile("pv"), site.DeviceSpec{Unit: 1}, "main", "grid")
	src := &fnSource{clock: e.clock}
	good := collector.Settings{
		Site: s, Connection: moreConn, Location: berlin,
		Sources: map[string]collector.Source{"d1": src},
	}
	st := storagemocks.NewMockCollectorStore(t)

	_, err := collector.New(collector.Settings{}, collector.Deps{})
	require.Error(t, err)

	unknown := good
	unknown.Connection = "tcp://nowhere:502"
	_, err = collector.New(unknown, e.deps(t, st, quietReporter(t)))
	require.ErrorContains(t, err, "unknown connection")

	nosrc := good
	nosrc.Sources = nil
	_, err = collector.New(nosrc, e.deps(t, st, quietReporter(t)))
	require.ErrorContains(t, err, "no source for device d1")

	c, err := collector.New(good, e.deps(t, st, quietReporter(t)))
	require.NoError(t, err)
	assert.NoError(t, c.Stop(), "stop without start")
	assert.Equal(t, health.Healthy, c.State())
}

func TestStartFailsWhenStoreFails(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, integProfile("pv"), site.DeviceSpec{Unit: 1}, "main", "grid")
	srcs := map[string]collector.Source{"d1": &fnSource{clock: e.clock}}
	boom := errors.New("boom")

	st := storagemocks.NewMockCollectorStore(t)
	st.EXPECT().EnsureSeries(mock.Anything, mock.Anything, mock.Anything).Return(nil, boom)
	err := e.buildWith(t, st, s, srcs).Start(context.Background())
	require.ErrorIs(t, err, boom)

	st = storagemocks.NewMockCollectorStore(t)
	st.EXPECT().EnsureSeries(mock.Anything, mock.Anything, mock.Anything).
		Return(map[storage.SeriesKey]int64{}, nil)
	st.EXPECT().CounterStates(mock.Anything, mock.Anything).Return(nil, boom)
	err = e.buildWith(t, st, s, srcs).Start(context.Background())
	require.ErrorIs(t, err, boom)

	st = storagemocks.NewMockCollectorStore(t)
	st.EXPECT().EnsureSeries(mock.Anything, mock.Anything, mock.Anything).
		Return(map[storage.SeriesKey]int64{}, nil)
	st.EXPECT().CounterStates(mock.Anything, mock.Anything).
		Return(map[int64]storage.CounterState{}, nil)
	st.EXPECT().LastStatus(mock.Anything, mock.Anything).Return(nil, boom)
	err = e.buildWith(t, st, s, srcs).Start(context.Background())
	require.ErrorIs(t, err, boom)
}

// mockStore returns ids for every ensured series and no persisted counter state.
func mockStore(t *testing.T) *storagemocks.MockCollectorStore {
	t.Helper()
	st := storagemocks.NewMockCollectorStore(t)
	st.EXPECT().EnsureSeries(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, _ int64, defs []storage.SeriesDef) (map[storage.SeriesKey]int64,
			error,
		) {
			out := map[storage.SeriesKey]int64{}
			for i, d := range defs {
				out[d.SeriesKey] = int64(i + 1)
			}
			return out, nil
		})
	st.EXPECT().CounterStates(mock.Anything, mock.Anything).
		Return(map[int64]storage.CounterState{}, nil)
	st.EXPECT().LastStatus(mock.Anything, mock.Anything).
		Return(map[string]storage.StatusEvent{}, nil)
	return st
}

func batchWh(b storage.Batch) float64 {
	sum := 0.0
	for _, x := range b.Energy {
		sum += x.Wh
	}
	return sum
}

func TestFlushFailureIsRetriedAndStatusErrorTolerated(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 6, 10, 11, 50, 0, 0, berlin)
	r := newRig(t, "meter", start)
	e := &env{clock: r.clock}
	var err error
	e.cache, err = livecache.New(eventbus.New(), r.clock)
	require.NoError(t, err)
	st := mockStore(t)
	st.EXPECT().AddStatus(mock.Anything, mock.Anything).Return(errors.New("no status")).Maybe()
	var mu sync.Mutex
	var batches []storage.Batch
	st.EXPECT().Flush(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, b storage.Batch) error {
			mu.Lock()
			defer mu.Unlock()
			batches = append(batches, b)
			if len(batches) == 1 {
				return errors.New("disk full")
			}
			return nil
		})
	c := e.buildWith(t, st, r.site, map[string]collector.Source{"wr1": r.src})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, c.Start(ctx))
	r.runUntil(start.Add(6 * time.Minute))
	require.NoError(t, c.Stop())
	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(batches), 2)
	assert.Greater(t, batchWh(batches[1]), 0.0)
	assert.GreaterOrEqual(t, batchWh(batches[1]), batchWh(batches[0]), "restored and retried")
}

func TestOfflineDeviceIsReported(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, integProfile("pv"), site.DeviceSpec{Unit: 1}, "main", "grid")
	src := &fnSource{clock: e.clock, f: func(time.Time) (map[int]uint16, error) {
		return nil, errors.New("timeout")
	}}
	rep := healthmocks.NewMockReporter(t)
	rep.EXPECT().DevicePoll(mock.Anything, mock.Anything).Maybe()
	rep.EXPECT().DeviceState("d1", health.DeviceOffline).Once()
	c, err := collector.New(collector.Settings{
		Site: s, Connection: moreConn, Location: berlin,
		Sources: map[string]collector.Source{"d1": src},
	}, e.deps(t, e.store.Collector(), rep))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, c.Start(ctx))
	assert.True(t, moreStart.Equal(c.LastBeat()))
	e.runUntil(moreStart.Add(time.Minute), pollInterval)
	require.NoError(t, c.Stop())
	assert.Equal(t, health.Recovering, c.State())
	assert.False(t, c.LastBeat().Before(moreStart.Add(time.Minute-pollInterval)))
	last, err := e.store.Collector().LastStatus(context.Background(), "d1")
	require.NoError(t, err)
	assert.Equal(t, storage.ConnectionStopped, last[storage.ConnectionRegister].Code)
	ev, err := e.store.Read().StatusEvents(context.Background(), "d1", 0,
		moreStart.Add(time.Hour).UnixMilli(), 10)
	require.NoError(t, err)
	require.Len(t, ev, 2)
	assert.Equal(t, storage.ConnectionOffline, ev[1].Code, "offline is stored")
}

// TestShareOneStringGetsOnlyItsPart: with only pv1 configured, pv2's power still weighs
// the split (it is read as a dependency), so pv1 books its third, not the whole field.
func TestShareOneStringGetsOnlyItsPart(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 6, 10, 6, 50, 0, 0, berlin)
	r := newRig(t, "meter", start)
	dev := solisSite(t, "meter").Devices["wr1"].Profile
	s := buildSite(t, dev, map[string]site.DeviceSpec{"wr1": {Unit: 1, Rollover: "23:59"}},
		map[string]site.MeterSpec{
			"pv2":  {Device: "wr1", Source: "pv2"},
			"grid": {Device: "wr1", Source: "grid"},
		})
	e := &env{clock: r.clock, store: r.store}
	cache, err := livecache.New(eventbus.New(), r.clock)
	require.NoError(t, err)
	e.cache = cache
	c := e.build(t, s, map[string]collector.Source{"wr1": pvShareSource{r.src}})
	e.runStopped(t, c, start.Add(6*time.Hour))
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, berlin)
	pv2 := e.total(t, "pv2", "energy_out", day, day.AddDate(0, 0, 1))
	assert.InDelta(t, float64(reg(r.src.model.pv))*100/3, pv2.Wh, 10, "750 of 2250 W")
}

// TestUnusedBlocksAreNotTracked: blocks no meter uses (battery, backup, strings) get no
// hidden series and no counter state; only the share base of a used block would.
func TestUnusedBlocksAreNotTracked(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 6, 10, 6, 50, 0, 0, berlin)
	r := newRig(t, "meter", start)
	dev := solisSite(t, "meter").Devices["wr1"].Profile
	s := buildSite(t, dev, map[string]site.DeviceSpec{"wr1": {Unit: 1, Rollover: "23:59"}},
		map[string]site.MeterSpec{"grid": {Device: "wr1", Source: "grid"}})
	e := &env{clock: r.clock, store: r.store}
	cache, err := livecache.New(eventbus.New(), r.clock)
	require.NoError(t, err)
	e.cache = cache
	c := e.build(t, s, map[string]collector.Source{"wr1": pvShareSource{r.src}})
	e.runStopped(t, c, start.Add(time.Hour))
	ctx := context.Background()
	for _, k := range []storage.SeriesKey{
		{Meter: "wr1:battery", Channel: "energy_in"},
		{Meter: "wr1:backup", Channel: "energy_in"},
		{Meter: "wr1:pv", Channel: "energy_out"},
	} {
		_, err := e.store.Read().SeriesID(ctx, k)
		require.ErrorIs(t, err, storage.ErrNotFound, k.Meter)
	}
	_, err = e.store.Read().SeriesID(ctx, storage.SeriesKey{Meter: "grid", Channel: "energy_in"})
	require.NoError(t, err)
}

// TestStatusStoresOnlyChanges: a restart restores the last codes (no duplicate rows), the
// connection is stored on change and marked stopped when the collector stops.
func TestStatusStoresOnlyChanges(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	prof := integProfile("pv") + "status:\n  fault: { addr: 102, type: u16, nonzero: fault, " +
		"text: { en: Fault, de: Fehler } }\n"
	s := oneDevice(t, prof, site.DeviceSpec{Unit: 1}, "main", "grid")
	flip := moreStart.Add(5 * time.Minute)
	src := &fnSource{clock: e.clock, f: func(now time.Time) (map[int]uint16, error) {
		code := uint16(0)
		if !now.Before(flip) {
			code = 5
		}
		return map[int]uint16{100: 1, 101: 2300, 102: code}, nil
	}}
	e.runStopped(t, e.build(t, s, map[string]collector.Source{"d1": src}),
		moreStart.Add(10*time.Minute))
	e.runStopped(t, e.build(t, s, map[string]collector.Source{"d1": src}),
		moreStart.Add(20*time.Minute))

	ev, err := e.store.Read().StatusEvents(context.Background(), "d1", 0,
		moreStart.Add(time.Hour).UnixMilli(), 100)
	require.NoError(t, err)
	codes := map[string][]int64{}
	for i := len(ev) - 1; i >= 0; i-- { // oldest first
		codes[ev[i].Register] = append(codes[ev[i].Register], ev[i].Code)
	}
	assert.Equal(t, []int64{0, 5}, codes["fault"], "the restart stores no unchanged code")
	assert.Equal(t, []int64{
		storage.ConnectionOnline, storage.ConnectionStopped,
		storage.ConnectionOnline, storage.ConnectionStopped,
	}, codes[storage.ConnectionRegister])
}
