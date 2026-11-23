package sitecalc_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/eventbus"
	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/sitecalc"
	"github.com/dombyte/pvmon/internal/util/clocktest"
	"github.com/dombyte/pvmon/profiles"
)

const (
	inputInterval = 10 * time.Second
	wait          = 5 * time.Second
	tick          = time.Millisecond
)

func loadProfile(t *testing.T, id string) *profile.Loaded {
	t.Helper()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	p, err := l.Load(id, ".")
	require.NoError(t, err)
	return p
}

func pvGridSite(t *testing.T) *site.Site {
	t.Helper()
	s, err := site.Build(site.Spec{
		Name: "Home",
		Connections: map[string]site.ConnectionSpec{
			"lan": {Address: "tcp://10.0.0.20", Timeout: 2 * time.Second},
		},
		Devices: map[string]site.DeviceSpec{
			"wr1": {
				Connection: "lan", Unit: 1, Profile: loadProfile(t, "solis/s6-hybrid"),
				Interval: 5 * time.Second, Rollover: "23:59",
			},
			"meter": {
				Address: "http://10.0.0.31", Profile: loadProfile(t, "shelly/pro-3em"),
				Interval: 2 * time.Second, Timeout: time.Second,
			},
		},
		Meters: map[string]site.MeterSpec{
			"pv":   {Device: "wr1", Source: "pv"},
			"grid": {Device: "meter", Source: "total", Critical: true},
		},
	})
	require.NoError(t, err)
	return s
}

type env struct {
	clock *clocktest.Clock
	cache *livecache.Cache
	calc  *sitecalc.Calculator
}

func newEnv(t *testing.T) *env {
	t.Helper()
	clock := clocktest.New(time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	bus := eventbus.New()
	cache, err := livecache.New(bus, clock)
	require.NoError(t, err)
	calc, err := sitecalc.New(pvGridSite(t), cache, bus, clock)
	require.NoError(t, err)
	return &env{clock: clock, cache: cache, calc: calc}
}

func (e *env) start(t *testing.T) {
	t.Helper()
	require.NoError(t, e.calc.Start(context.Background()))
	t.Cleanup(func() { assert.NoError(t, e.calc.Stop()) })
	// only the refresh ticker stays active once the loop is idle
	require.Eventually(t, func() bool { return e.clock.Waiters() == 1 }, wait, tick)
}

func (e *env) setInputs(pv, grid float64) {
	now := e.clock.Now()
	e.cache.Set("pv/power", livecache.Value{Num: pv, At: now}, inputInterval)
	e.cache.Set("grid/power", livecache.Value{Num: grid, At: now}, inputInterval)
}

func (e *env) num(key string) (float64, bool) {
	v, ok := e.cache.Get(key)
	return v.Num, ok
}

func TestNewRejectsNilDependencies(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := pvGridSite(t)
	bus := eventbus.New()
	tests := []struct {
		name  string
		build func() (*sitecalc.Calculator, error)
	}{
		{"site", func() (*sitecalc.Calculator, error) { return sitecalc.New(nil, e.cache, bus, e.clock) }},
		{"cache", func() (*sitecalc.Calculator, error) { return sitecalc.New(s, nil, bus, e.clock) }},
		{"bus", func() (*sitecalc.Calculator, error) { return sitecalc.New(s, e.cache, nil, e.clock) }},
		{"clock", func() (*sitecalc.Calculator, error) { return sitecalc.New(s, e.cache, bus, nil) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := tt.build()
			require.Error(t, err)
			assert.Nil(t, c)
			assert.Contains(t, err.Error(), tt.name)
		})
	}
}

func TestDebouncedRecalculation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.start(t)
	e.setInputs(3000, -1000)
	// the debounce timer joins the refresh ticker
	require.Eventually(t, func() bool { return e.clock.Waiters() == 2 }, wait, tick)
	_, ok := e.num("site/pv.power")
	assert.False(t, ok, "nothing before the debounce elapsed")

	e.clock.Advance(sitecalc.Debounce)
	require.Eventually(t, func() bool {
		_, ok := e.num("site/consumption.power")
		return ok
	}, wait, tick)
	for key, want := range map[string]float64{
		"site/pv.power": 3000, "site/grid_in.power": 0, "site/grid_out.power": 1000,
		"site/consumption.power": 2000, "site/house_rest.power": 2000,
	} {
		got, ok := e.num(key)
		assert.True(t, ok, key)
		assert.InDelta(t, want, got, 1e-9, key)
	}
}

func TestOwnOutputsDoNotRetrigger(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.start(t)
	// A site/ event is ignored; the meter event behind it (events are handled in order)
	// is the first that arms the debounce, so the timer appearing proves the site/ event
	// was handled without one.
	e.cache.Set("site/pv.power", livecache.Value{Num: 1, At: e.clock.Now()}, inputInterval)
	e.setInputs(3000, -1000)
	require.Eventually(t, func() bool { return e.clock.Waiters() == 2 }, wait, tick)
	_, ok := e.num("site/consumption.power")
	assert.False(t, ok, "nothing before the debounce elapsed")

	e.clock.Advance(sitecalc.Debounce)
	require.Eventually(t, func() bool {
		v, ok := e.num("site/pv.power")
		return ok && v == 3000
	}, wait, tick)
	// one recalculation: the debounce timer is spent, only the refresh ticker remains
	require.Eventually(t, func() bool { return e.clock.Waiters() == 1 }, wait, tick)
}

func TestStaleInputRemovesQuantities(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.start(t)
	e.setInputs(3000, 500)
	require.Eventually(t, func() bool { return e.clock.Waiters() == 2 }, wait, tick)
	e.clock.Advance(sitecalc.Debounce)
	require.Eventually(t, func() bool {
		_, ok := e.num("site/house_rest.power")
		return ok
	}, wait, tick)

	// inputs are older than 3 × interval at the next refresh
	e.clock.Advance(4 * inputInterval)
	require.Eventually(t, func() bool {
		_, ok := e.num("site/house_rest.power")
		return !ok
	}, wait, tick)
	for _, key := range []string{"site/pv.power", "site/grid_in.power", "site/consumption.power"} {
		_, ok := e.num(key)
		assert.False(t, ok, key)
	}
}

func TestLifecycle(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	assert.Equal(t, health.Healthy, e.calc.State())
	require.NoError(t, e.calc.Stop(), "stop before start")

	e.start(t)
	first := e.calc.LastBeat()
	assert.True(t, e.clock.Now().Equal(first))
	e.clock.Advance(sitecalc.Refresh)
	require.Eventually(t, func() bool { return e.calc.LastBeat().After(first) }, wait, tick)
	assert.Equal(t, health.Healthy, e.calc.State())

	require.NoError(t, e.calc.Stop())
	require.NoError(t, e.calc.Stop(), "idempotent")
}
