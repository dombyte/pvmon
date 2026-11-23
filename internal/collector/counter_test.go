package collector

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
)

var berlin = func() *time.Location {
	l, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic(err)
	}
	return l
}()

var t0 = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func sec(n int) time.Time { return t0.Add(time.Duration(n) * time.Second) }

func TestLifetime(t *testing.T) {
	t.Parallel()
	cfg := CounterConfig{Resolution: 1, MaxPower: 10000} // 10 kW → 13.9 Wh per 5 s
	type step struct {
		v    float64
		at   int
		gap  bool
		want Delta
	}
	cases := map[string][]step{
		"normal": {
			{1000, 0, false, Delta{}},
			{1010, 5, false, Delta{Wh: 10}},
			{1010, 10, false, Delta{}},
			{1020, 3605, true, Delta{Wh: 10, Gap: true}},
		},
		"jitter": {
			{1000, 0, false, Delta{}},
			{999.5, 5, false, Delta{}},
			{1005, 10, false, Delta{Wh: 5}},
		},
		"replaced": {
			{1000, 0, false, Delta{}},
			{10, 5, false, Delta{Event: EventReplaced}},
			{15, 10, false, Delta{Wh: 5}},
		},
		"glitch": {
			{1000, 0, false, Delta{}},
			{900000, 5, false, Delta{Held: true}},
			{1012, 10, false, Delta{Wh: 12}},
		},
		"jump": {
			{1000, 0, false, Delta{}},
			{900000, 5, false, Delta{Held: true}},
			{900010, 10, false, Delta{Event: EventJump}},
			{900020, 15, false, Delta{Wh: 10}},
		},
		"jumpdown": {
			{1000, 0, false, Delta{}},
			{900000, 5, false, Delta{Held: true}},
			{800000, 10, false, Delta{Held: true}},
		},
	}
	for name, steps := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			l := NewLifetime(cfg, nil, time.Time{})
			for i, s := range steps {
				assert.Equal(t, s.want, l.Update(s.v, sec(s.at), s.gap), "step %d", i)
			}
		})
	}
}

// TestLifetimeStepLag: a 10 Wh counter at 20 kW (27.8 Wh per 5 s) can show 60 Wh after
// 10 s, because the energy below one step at the previous reading is counted later. That
// is no jump: the delta is booked (regression: Gate 1 lost 60 Wh at 10:18 on 2026-11-21).
func TestLifetimeStepLag(t *testing.T) {
	t.Parallel()
	l := NewLifetime(CounterConfig{Step: 10, MaxPower: 20000}, nil, time.Time{})
	assert.Equal(t, Delta{}, l.Update(1000, sec(0), false))
	assert.Equal(t, Delta{Held: true}, l.Update(1060, sec(5), false), "37.8 Wh limit")
	assert.Equal(t, Delta{Wh: 60}, l.Update(1060, sec(10), false), "65.6 Wh limit")
	assert.Equal(t, Delta{Wh: 30}, l.Update(1090, sec(15), false), "37.8 Wh limit")
}

func TestCounterConfigStep(t *testing.T) {
	t.Parallel()
	d := &site.Device{Profile: &profile.Loaded{Profile: &profile.Profile{MaxPower: 70000}}}
	scale := 0.01
	cases := map[string]struct {
		ch   profile.Channel
		step float64
	}{
		"u32 kWh":  {profile.Channel{Type: "u32", Unit: "kWh", Scale: &scale}, 10},
		"u16 Wh":   {profile.Channel{Type: "u16", Unit: "Wh"}, 1},
		"f32 kWh":  {profile.Channel{Type: "f32", Unit: "kWh"}, 0},
		"json kWh": {profile.Channel{Type: "number", Unit: "kWh"}, 0},
	}
	for name, c := range cases {
		assert.InDelta(t, c.step, counterConfig(d, c.ch).Step, 1e-9, name)
	}
}

func TestLifetimeOverflowAndRestore(t *testing.T) {
	t.Parallel()
	wrap := float64(math.MaxUint32) + 1
	base := wrap - 3
	l := NewLifetime(CounterConfig{MaxPower: 10000, Wrap: wrap}, &base, sec(0))
	got, at := l.Base()
	require.NotNil(t, got)
	assert.Equal(t, sec(0), at)
	assert.Equal(t, Delta{Wh: 5, Event: EventOverflow}, l.Update(2, sec(5), false))
	v, _ := NewLifetime(CounterConfig{}, nil, time.Time{}).Base()
	assert.Nil(t, v)
}

func TestEffectiveResolution(t *testing.T) {
	t.Parallel()
	// f32 kWh: 1 Wh steps below ~16 384 kWh, 2 Wh above, 4 Wh above 32 768 kWh (spec 5.3)
	c := CounterConfig{Float32: true, UnitFactor: 1000}
	assert.InDelta(t, 0.98, c.EffectiveResolution(10_000_000), 0.01)
	assert.InDelta(t, 1.95, c.EffectiveResolution(20_000_000), 0.01)
	assert.InDelta(t, 3.9, c.EffectiveResolution(40_000_000), 0.01)
	assert.InDelta(t, 100, CounterConfig{Resolution: 100}.EffectiveResolution(5), 0)
	assert.Greater(t, CounterConfig{Float32: true}.EffectiveResolution(1), 0.0)
}

func TestIntegrator(t *testing.T) {
	t.Parallel()
	var i Integrator
	_, ok := i.Update(1000, sec(0), false)
	assert.False(t, ok)
	wh, ok := i.Update(2000, sec(3600), false)
	require.True(t, ok)
	assert.InDelta(t, 1500, wh, 1e-9)
	_, ok = i.Update(2000, sec(3605), true)
	assert.False(t, ok, "gap stays a gap")
	wh, ok = i.Update(-1000, sec(3605+1800), false)
	require.True(t, ok)
	assert.InDelta(t, 250, wh, 1e-9)
	_, ok = i.Update(0, sec(3605+1800), false)
	assert.False(t, ok, "no time passed")
	i.Reset()
	_, ok = i.Update(0, sec(9000), false)
	assert.False(t, ok)
}
