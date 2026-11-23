package simulate

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/util/clocktest"
	"github.com/dombyte/pvmon/profiles"
)

func TestPowers(t *testing.T) {
	t.Parallel()
	noon := PowersAt(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
	assert.InDelta(t, peakPV, noon.PV, 1)
	assert.InDelta(t, -battPower, noon.Battery, 0)
	assert.InDelta(t, noon.House-noon.PV+battPower, noon.Grid, 1e-9)
	evening := PowersAt(time.Date(2026, 6, 1, 19, 0, 0, 0, time.UTC))
	assert.InDelta(t, chargePower, evening.Wallbox, 0)
	assert.InDelta(t, 0, evening.PV, 0)
	assert.Greater(t, evening.Grid, chargePower-battPower)
	for _, role := range []string{"pv", "battery", "grid", "inverter", "phase", "backup"} {
		_ = powerFor(noon, role, "")
	}
	assert.InDelta(t, 0, powerFor(noon, "consumer", "heatpump"), 0, "off at noon")
	assert.InDelta(t, noon.House, powerFor(noon, "consumer", "other"), 0)
	night := PowersAt(time.Date(2026, 6, 1, 22, 0, 0, 0, time.UTC))
	assert.InDelta(t, heatPump, powerFor(night, "consumer", "heatpump"), 0)
	assert.InDelta(t, night.House+heatPump-night.Battery, night.Grid, 1e-9)
	assert.InDelta(t, 1, energyPower("grid", "energy_in", 1), 0)
	assert.InDelta(t, -1, energyPower("grid", "energy_out", 1), 0)
	assert.InDelta(t, 1, energyPower("pv", "energy_out", 1), 0)
	assert.InDelta(t, -1, energyPower("battery", "energy_in", 1), 0)
}

// TestSourcesDecode simulates every built-in profile for an hour and decodes the result:
// counters only rise and the grid power follows the model.
func TestSourcesDecode(t *testing.T) {
	t.Parallel()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	loc := time.UTC
	clock := clocktest.New(time.Date(2026, 6, 1, 11, 30, 0, 0, loc))
	for _, id := range l.IDs() {
		p, err := l.Load(id, ".")
		require.NoError(t, err)
		addr := "http://sim"
		if p.Transport == profile.TransportModbus {
			addr = "tcp://sim"
		}
		dev := &site.Device{ID: "d", Profile: p, Interval: 5 * time.Second}
		s := &site.Site{
			Devices:     map[string]*site.Device{"d": dev},
			Connections: map[string]*site.Connection{addr: {Address: addr, Devices: []string{"d"}}},
			Meters:      map[string]*site.Meter{},
		}
		srcs, tr, err := Sources(loc)(s, s.Connections[addr], clock, zerolog.Nop())
		require.NoError(t, err)
		tr.Start()
		tr.Stop()
		first, err := srcs["d"].Fetch(context.Background())
		require.NoError(t, err, id)
		clock.Advance(time.Hour)
		second, err := srcs["d"].Fetch(context.Background())
		require.NoError(t, err, id)
		assertRising(t, p.Profile, first, second)
	}
}

func assertRising(t *testing.T, p *profile.Profile, a, b profile.Sample) {
	t.Helper()
	ra, err := a.Raw()
	require.NoError(t, err)
	rb, err := b.Raw()
	require.NoError(t, err)
	for bn, blk := range p.Blocks {
		va, vb := p.Channels(bn, ra), p.Channels(bn, rb)
		for cn, c := range blk.Channels {
			if c.Counter != profile.CounterNone {
				assert.GreaterOrEqual(t, vb[cn].Num, va[cn].Num, "%s %s.%s", p.ID, bn, cn)
			}
		}
	}
}

func TestCounterDailyReset(t *testing.T) {
	t.Parallel()
	c := &counter{daily: true, day: 1}
	day1 := time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)
	assert.InDelta(t, 5, c.add(5, day1), 0)
	assert.InDelta(t, 2, c.add(2, day1.Add(2*time.Hour)), 0, "reset on the next day")
	assert.InDelta(t, 2, c.add(-1, day1.Add(2*time.Hour)), 0, "never decreases")
}

func TestSetups(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"R1", "R2", "R3", "R4", "R5", "R6", "R7"} {
		data, err := Setup(name)
		require.NoError(t, err)
		assert.Contains(t, string(data), "simulate.db")
	}
	_, err := Setup("R9")
	require.ErrorContains(t, err, "known: [R1 R2 R3 R4 R5 R6 R7]")
	assert.Equal(t, "x", insertDevices("x", "\n a"))
}

// TestSourceShares: configured strings share the PV power, unused strings stay dark, and
// consumers of one category split its power.
func TestSourceShares(t *testing.T) {
	t.Parallel()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	load := func(id string) *profile.Loaded {
		p, err := l.Load(id, ".")
		require.NoError(t, err)
		return p
	}
	s, err := site.Build(site.Spec{
		Name:        "Home",
		Connections: map[string]site.ConnectionSpec{"lan": {Address: "tcp://sim", Timeout: time.Second}},
		Devices: map[string]site.DeviceSpec{
			"wr1": {Connection: "lan", Unit: 1, Profile: load("solis/s6-hybrid"), Interval: time.Second},
			"wb1": {Address: "http://wb1", Profile: load("goe/charger-v2"), Interval: time.Second},
			"wb2": {Address: "http://wb2", Profile: load("goe/charger-v2"), Interval: time.Second},
		},
		Meters: map[string]site.MeterSpec{
			"pv":   {Device: "wr1", Source: "pv"},
			"s1":   {Device: "wr1", Source: "pv1", Parent: "pv"},
			"s2":   {Device: "wr1", Source: "pv2", Parent: "pv"},
			"grid": {Device: "wr1", Source: "grid"},
			"wb1":  {Device: "wb1", Source: "charger"}, "wb2": {Device: "wb2", Source: "charger"},
		},
	})
	require.NoError(t, err)
	clock := clocktest.New(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
	inv := NewSource(s, s.Devices["wr1"], clock, time.UTC)
	assert.InDelta(t, 0.5, inv.shares["pv1"], 0)
	assert.InDelta(t, 0.5, inv.shares["pv2"], 0)
	assert.InDelta(t, 0, inv.shares["pv3"], 0, "no meter: dark")
	assert.InDelta(t, 1, inv.shares["pv"], 0, "the only PV device carries the whole field")
	assert.InDelta(t, 0.5, NewSource(s, s.Devices["wb1"], clock, time.UTC).shares["charger"], 0)
}
