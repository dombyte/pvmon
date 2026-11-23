package collector

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// solisDaily is a 0.1 kWh daily counter (100 Wh resolution) with a 20 kW guard.
var solisDaily = CounterConfig{Resolution: 100, MaxPower: 20000}

func local(y int, m time.Month, d, h, mi, s int) time.Time {
	return time.Date(y, m, d, h, mi, s, 0, berlin)
}

type poll struct {
	at   time.Time
	vals []float64 // NaN-free; -1 = missing
	gap  bool
	want []Delta
}

func runGroup(t *testing.T, g *Group, polls []poll) {
	t.Helper()
	for i, p := range polls {
		rs := make([]Reading, len(p.vals))
		for j, v := range p.vals {
			rs[j] = Reading{Value: v, Valid: v >= 0}
		}
		assert.Equal(t, p.want, g.Update(p.at, p.gap, rs), "poll %d at %s", i, p.at)
	}
}

func newGroup(n int, rollover time.Duration) *Group {
	ms := make([]*Daily, n)
	for i := range ms {
		ms[i] = NewDaily(solisDaily, nil, time.Time{})
	}
	return NewGroup(ms, rollover, berlin, time.Time{}, time.Time{})
}

func d(wh float64) Delta { return Delta{Wh: wh} }

var none, held = Delta{}, Delta{Held: true}

func reset(wh float64, from time.Time) Delta {
	return Delta{Wh: wh, ResetSeen: true, From: from}
}

func TestDailyMidnightReset(t *testing.T) {
	t.Parallel()
	g := newGroup(3, 0)
	before := local(2026, 11, 18, 23, 59, 55)
	runGroup(t, g, []poll{
		{local(2026, 11, 18, 23, 59, 50), []float64{5000, 3000, 0}, false, []Delta{none, none, none}},
		{before, []float64{5100, 3000, 0}, false, []Delta{d(100), none, none}},
		// the drop is pending: every member is held, the counters buffer the energy …
		{local(2026, 11, 19, 0, 0, 5), []float64{0, 0, 100}, false, []Delta{held, held, held}},
		{local(2026, 11, 19, 0, 10, 0), []float64{100, 0, 200}, false, []Delta{held, held, held}},
		// … until it persisted for 15 min: each member books its value since the reset,
		// from the poll before the drop
		{local(2026, 11, 19, 0, 15, 5), []float64{200, 0, 300}, false, []Delta{
			reset(200, before), reset(0, before), reset(300, before),
		}},
		{local(2026, 11, 19, 0, 15, 10), []float64{300, 0, 300}, false, []Delta{d(100), none, none}},
	})
	assert.Equal(t, local(2026, 11, 19, 0, 0, 5), g.LastReset())
	assert.Equal(t, g.LastReset(), g.PeriodStart(local(2026, 11, 19, 12, 0, 0)))
}

func TestDailyGlitches(t *testing.T) {
	t.Parallel()
	g := newGroup(2, 0)
	runGroup(t, g, []poll{
		{local(2026, 11, 19, 14, 0, 0), []float64{20000, 8000}, false, []Delta{none, none}},
		// one register reads 0: pending, held …
		{local(2026, 11, 19, 14, 0, 5), []float64{0, 8000}, false, []Delta{held, none}},
		// … and back: a glitch
		{local(2026, 11, 19, 14, 0, 10), []float64{20000, 8100}, false, []Delta{none, d(100)}},
		// all registers read 0 for a while …
		{local(2026, 11, 19, 14, 0, 15), []float64{0, 0}, false, []Delta{held, held}},
		{local(2026, 11, 19, 14, 10, 0), []float64{0, 0}, false, []Delta{held, held}},
		// … and come back before 15 min: no reset, nothing counted twice
		{local(2026, 11, 19, 14, 10, 5), []float64{20000, 8100}, false, []Delta{none, none}},
		{local(2026, 11, 19, 14, 10, 10), []float64{20100, -1}, false, []Delta{d(100), none}},
	})
	assert.True(t, g.LastReset().IsZero())
}

// A register that keeps its old value must not block the reset of the others.
func TestDailyResetPartial(t *testing.T) {
	t.Parallel()
	g := newGroup(2, 0)
	before := local(2026, 11, 18, 23, 59, 55)
	runGroup(t, g, []poll{
		{before, []float64{5000, 3000}, false, []Delta{none, none}},
		{local(2026, 11, 19, 0, 0, 5), []float64{0, 3000}, false, []Delta{held, none}},
		{local(2026, 11, 19, 0, 15, 10), []float64{100, 3000}, false, []Delta{
			{Wh: 100, ResetSeen: true, From: before, Event: EventResetPartial}, none,
		}},
		{local(2026, 11, 19, 0, 15, 15), []float64{200, 3100}, false, []Delta{d(100), d(100)}},
	})
}

func TestDailyResetMissing(t *testing.T) {
	t.Parallel()
	base := 1000.0
	m := NewDaily(solisDaily, &base, local(2026, 11, 18, 12, 0, 0))
	g := NewGroup([]*Daily{m}, 0, berlin, local(2026, 11, 18, 12, 0, 0),
		local(2026, 11, 18, 0, 0, 0))
	runGroup(t, g, []poll{
		{local(2026, 11, 19, 0, 59, 0), []float64{1100}, false, []Delta{d(100)}},
		// reported once, 25 h after the last reset; nothing is held
		{local(2026, 11, 19, 1, 0, 5), []float64{1200}, false, []Delta{
			{Wh: 100, Event: EventResetMissing},
		}},
		{local(2026, 11, 19, 1, 0, 10), []float64{1200}, false, []Delta{none}},
	})
}

// After a gap over midnight the new day's energy starts at the expected reset (here the
// rollover 00:00), not at the last poll of the previous evening.
func TestDailyGapOverMidnight(t *testing.T) {
	t.Parallel()
	g := newGroup(2, 0)
	last := local(2026, 11, 18, 22, 0, 0)
	gapReset := func(wh float64) Delta {
		return Delta{Wh: wh, Gap: true, ResetSeen: true, From: local(2026, 11, 19, 0, 0, 0)}
	}
	runGroup(t, g, []poll{
		{last, []float64{9000, 0}, false, []Delta{none, none}},
		{local(2026, 11, 19, 8, 0, 0), []float64{300, 200}, true, []Delta{held, held}},
		{local(2026, 11, 19, 8, 15, 0), []float64{400, 200}, false, []Delta{
			gapReset(400), gapReset(200),
		}},
	})
}

// A restart after a short run that already saw the drop: the poll before the new drop is
// after the reset, so the reset is placed at the learned reset time of the last one.
func TestDailyRestartAfterSeenDrop(t *testing.T) {
	t.Parallel()
	base := 9000.0
	m := NewDaily(solisDaily, &base, local(2026, 11, 18, 22, 45, 0))
	g := NewGroup([]*Daily{m}, 0, berlin, local(2026, 11, 19, 7, 35, 0),
		local(2026, 11, 18, 0, 0, 7))
	runGroup(t, g, []poll{
		{local(2026, 11, 19, 8, 30, 0), []float64{2000}, true, []Delta{held}},
		{local(2026, 11, 19, 8, 45, 0), []float64{2100}, false, []Delta{
			{Wh: 2100, Gap: true, ResetSeen: true, From: local(2026, 11, 19, 0, 0, 7)},
		}},
	})
}

// The expected reset never lies before the last reading before the drop, and a member that
// could not show the reset still books its rise as a gap.
func TestDailyGapResetFloorAndStuckMember(t *testing.T) {
	t.Parallel()
	stuck, base := 500.0, 9000.0
	read := local(2026, 11, 19, 0, 30, 0)
	g := NewGroup([]*Daily{
		NewDaily(solisDaily, &stuck, read), NewDaily(solisDaily, &base, read),
	}, 0, berlin, read, local(2026, 11, 18, 0, 0, 0))
	runGroup(t, g, []poll{
		{local(2026, 11, 19, 6, 0, 0), []float64{550, 300}, true, []Delta{held, held}},
		{local(2026, 11, 19, 6, 15, 0), []float64{600, 400}, false, []Delta{
			{Wh: 100, Gap: true, Event: EventResetPartial},
			{Wh: 400, Gap: true, ResetSeen: true, From: read},
		}},
	})
}

// A restart during a pending reset resumes from the last poll before the crash (seen), not
// from the last change of the value, and books the same energy as without the restart.
func TestDailyRestartDuringPendingReset(t *testing.T) {
	t.Parallel()
	base := 5100.0
	seen := local(2026, 11, 19, 0, 0, 0)
	m := NewDaily(solisDaily, &base, local(2026, 11, 18, 23, 0, 0))
	g := NewGroup([]*Daily{m}, 0, berlin, seen, time.Time{})
	runGroup(t, g, []poll{
		{local(2026, 11, 19, 0, 3, 0), []float64{0}, true, []Delta{held}},
		{local(2026, 11, 19, 0, 18, 0), []float64{300}, false, []Delta{
			{Wh: 300, Gap: true, ResetSeen: true, From: seen},
		}},
	})
}

func TestDailyPeriodStartBeforeFirstReset(t *testing.T) {
	t.Parallel()
	g := newGroup(1, 23*time.Hour+59*time.Minute) // rollover "23:59"
	// DST end: the 25 h day still starts its device day at local 23:59
	at := local(2026, 10, 25, 23, 59, 30)
	assert.Equal(t, local(2026, 10, 25, 23, 59, 0), g.PeriodStart(at))
	assert.Equal(t, local(2026, 10, 24, 23, 59, 0), g.PeriodStart(at.Add(-time.Minute)))
}

func TestDailyRestoreAndImplausible(t *testing.T) {
	t.Parallel()
	base := 1000.0
	m := NewDaily(solisDaily, &base, local(2026, 11, 19, 10, 0, 0))
	v, at := m.Base()
	assert.InDelta(t, 1000, *v, 0)
	assert.Equal(t, 10, at.Hour())
	g := NewGroup([]*Daily{m}, 0, berlin, local(2026, 11, 19, 10, 0, 0), time.Time{})
	runGroup(t, g, []poll{
		{local(2026, 11, 19, 10, 0, 5), []float64{90000}, false, []Delta{held}},
		{local(2026, 11, 19, 10, 0, 10), []float64{1100}, false, []Delta{d(100)}},
	})
	nilBase, _ := NewDaily(solisDaily, nil, time.Time{}).Base()
	assert.Nil(t, nilBase)
}
