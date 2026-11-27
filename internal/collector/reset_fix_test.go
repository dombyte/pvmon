package collector

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestDailyResetWhenNewDayPassesOld: after a reset the counter climbs past yesterday's
// value before the reset is confirmed. It is still a reset: every member books its whole
// new value, also a member that is alone in its group.
func TestDailyResetWhenNewDayPassesOld(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		res, old float64
		climb    []float64 // readings after the reset; the last one confirms it
	}{
		"passes the old value":  {res: 100, old: 200, climb: []float64{0, 100, 200, 700}},
		"within one step of it": {res: 1000, old: 2000, climb: []float64{0, 0, 1000, 1000}},
	} {
		for _, members := range []int{1, 2} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				cfg := CounterConfig{Resolution: c.res, MaxPower: 20000}
				t0 := local(2026, 11, 18, 23, 59, 0)
				old, big := c.old, 50000.0
				ms := []*Daily{NewDaily(cfg, &old, t0), NewDaily(cfg, &big, t0)}[:members]
				g := NewGroup(ms, 0, berlin, t0, time.Time{})
				var booked float64
				var seen bool
				for i, v := range c.climb {
					rs := []Reading{{v, true}, {0, true}}[:members]
					at := t0.Add(time.Duration(i+1) * 6 * time.Minute)
					d := g.Update(at, false, rs)[0]
					booked, seen = booked+d.Wh, seen || d.ResetSeen
				}
				assert.True(t, seen, "the reset is confirmed")
				assert.InDelta(t, c.climb[len(c.climb)-1], booked, 0)
			})
		}
	}
}

// TestDailyZeroReadOfSmallCounterIsGlitch: a register that reads 0 once and then returns
// to its value, or just above it, is not a reset: only the rise is booked.
func TestDailyZeroReadOfSmallCounterIsGlitch(t *testing.T) {
	t.Parallel()
	g := newGroup(1, 0)
	at := func(m int) time.Time { return local(2026, 11, 19, 14, m, 0) }
	runGroup(t, g, []poll{
		{at(0), []float64{200}, false, []Delta{none}},
		{at(1), []float64{0}, false, []Delta{held}},
		{at(2), []float64{0}, false, []Delta{held}},
		{at(3), []float64{300}, false, []Delta{d(100)}},
		{at(30), []float64{300}, false, []Delta{none}},
	})
	assert.True(t, g.LastReset().IsZero())
}

// TestDailyGarbledReturnIsGlitch: a bad read that returns through a value no power can
// reach so fast is no climb after a reset: nothing is booked twice.
func TestDailyGarbledReturnIsGlitch(t *testing.T) {
	t.Parallel()
	g := newGroup(1, 0)
	at := func(m int) time.Time { return local(2026, 11, 19, 14, m, 0) }
	runGroup(t, g, []poll{
		{at(0), []float64{20000}, false, []Delta{none}},
		{at(1), []float64{0}, false, []Delta{held}},
		{at(2), []float64{5000}, false, []Delta{held}},
		{at(3), []float64{20000}, false, []Delta{none}},
		{at(30), []float64{20000}, false, []Delta{none}},
	})
	assert.True(t, g.LastReset().IsZero())
}
