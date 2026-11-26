package collector_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
)

// coarseProfile has a backup block with a 0.1 kWh daily counter and its power, as Solis.
const coarseProfile = profHead + `  backup:
    role: backup
    channels:
      power: { addr: 100, type: u16, unit: W }
      energy_in: { addr: 101, type: u16, scale: 0.1, unit: kWh, counter: daily_reset,
        reset_group: daily, resolution: 0.1 }
  grid:
    role: grid
    channels:
      power: { addr: 104, type: i32, unit: W }
`

// load is a backup load: power(t) in W, its energy since moreStart and the coarse counter.
type load struct {
	power func(time.Time) float64
	wh    float64
	last  time.Time
}

func (l *load) words(now time.Time) map[int]uint16 {
	if !l.last.IsZero() {
		l.wh += l.power(now) * now.Sub(l.last).Hours()
	}
	l.last = now
	return map[int]uint16{100: uint16(l.power(now)), 101: uint16(math.Floor(l.wh / 100))}
}

// coarseRun polls the load and fails in (failFrom, failTo]; the device runs on meanwhile.
type coarseRun struct {
	e              *env
	s              *site.Site
	l              *load
	failFrom, fail time.Time
}

func newCoarseRun(t *testing.T, power func(time.Time) float64) *coarseRun {
	t.Helper()
	e := newEnv(t, moreStart)
	return &coarseRun{
		e: e, s: oneDevice(t, coarseProfile, site.DeviceSpec{Unit: 1}, "backup", "grid"),
		l: &load{power: power},
	}
}

func (r *coarseRun) source() *fnSource {
	return &fnSource{clock: r.e.clock, f: func(now time.Time) (map[int]uint16, error) {
		w := r.l.words(now)
		if now.After(r.failFrom) && !now.After(r.fail) {
			return nil, context.DeadlineExceeded
		}
		return w, nil
	}}
}

// session runs one collector until end.
func (r *coarseRun) session(t *testing.T, end time.Time) {
	t.Helper()
	src := r.source()
	r.e.runStopped(t, r.e.build(t, r.s, map[string]collector.Source{"d1": src}), end)
}

// idle lets the device run on while pvmon is stopped.
func (r *coarseRun) idle(until time.Time) {
	for r.e.clock.Now().Before(until) {
		r.e.clock.Advance(pollInterval)
		r.l.words(r.e.clock.Now())
	}
}

// slot returns the booked total of the slot that starts at m minutes after moreStart.
func (r *coarseRun) slot(t *testing.T, m int) storage.EnergyTotal {
	t.Helper()
	from := moreStart.Add(time.Duration(m) * time.Minute)
	return r.e.total(t, "backup", "energy_in", from, from.Add(15*time.Minute))
}

func minutes(m int) time.Time { return moreStart.Add(time.Duration(m) * time.Minute) }

func constant(w float64) func(time.Time) float64 { return func(time.Time) float64 { return w } }

// TestCoarseStepFollowsPower: two idle hours, then 2 kW. The first 0.1 kWh step lands
// where the power flowed, not spread evenly over the idle hours.
func TestCoarseStepFollowsPower(t *testing.T) {
	t.Parallel()
	r := newCoarseRun(t, func(now time.Time) float64 {
		if now.Before(minutes(120)) {
			return 0
		}
		return 2000
	})
	r.session(t, minutes(135))
	idle := r.e.total(t, "backup", "energy_in", moreStart, minutes(120))
	assert.Equal(t, 8, idle.Buckets, "the idle slots are booked")
	// The trapezoid of the poll before the power started (0 W → 2 kW) belongs to 11:45.
	edge := 2000.0 / 2 * pollInterval.Hours()
	assert.InDelta(t, 0, idle.Wh, edge+1e-6, "no energy in the idle hours")
	assert.InDelta(t, 500, r.slot(t, 120).Wh, edge+1e-6, "15 min at 2 kW, in 0.1 kWh steps")
	assert.Zero(t, idle.Flags|r.slot(t, 120).Flags)
}

// TestShortFailureIsNotEstimated: one failed poll is a slow cycle, not an outage.
func TestShortFailureIsNotEstimated(t *testing.T) {
	t.Parallel()
	r := newCoarseRun(t, constant(1200))
	r.failFrom, r.fail = minutes(20), minutes(20).Add(pollInterval)
	r.session(t, minutes(60))
	tot := r.e.total(t, "backup", "energy_in", moreStart, minutes(60))
	assert.InDelta(t, 1200, tot.Wh, 100)
	assert.Zero(t, tot.Flags&(storage.FlagEstimated|storage.FlagGap))
}

// TestOutageFlagsOnlyItsSlots: a 15-minute outage flags the slots it covers, not the
// slots before it where the counter was read.
func TestOutageFlagsOnlyItsSlots(t *testing.T) {
	t.Parallel()
	r := newCoarseRun(t, constant(1200))
	r.failFrom, r.fail = minutes(20), minutes(35)
	r.session(t, minutes(60))
	assert.Zero(t, r.slot(t, 0).Flags&storage.FlagEstimated, "before the outage")
	assert.NotZero(t, r.slot(t, 15).Flags&storage.FlagEstimated, "the outage starts here")
	assert.NotZero(t, r.slot(t, 30).Flags&storage.FlagEstimated, "the outage ends here")
	assert.Zero(t, r.slot(t, 45).Flags&storage.FlagEstimated, "after the outage")
	tot := r.e.total(t, "backup", "energy_in", moreStart, minutes(60))
	assert.InDelta(t, 1200, tot.Wh, 100, "the counter covers the outage")
	assert.Zero(t, tot.Flags&storage.FlagGap)
}

// TestRestartEstimates: a quick restart flags nothing; a long one flags its downtime only,
// also when the delta that covers it is booked after the restart.
func TestRestartEstimates(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		down      time.Duration
		estimated bool
	}{
		"quick": {down: 20 * time.Second},
		"long":  {down: 20 * time.Minute, estimated: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newCoarseRun(t, constant(600))
			r.session(t, minutes(20))
			r.idle(minutes(20).Add(c.down))
			r.session(t, minutes(75))
			assert.Zero(t, r.slot(t, 0).Flags&storage.FlagEstimated, "before the stop")
			assert.Equal(t, c.estimated, r.slot(t, 30).Flags&storage.FlagEstimated != 0,
				"the downtime")
			assert.Zero(t, r.slot(t, 60).Flags&storage.FlagEstimated, "after the restart")
			tot := r.e.total(t, "backup", "energy_in", moreStart, minutes(75))
			assert.InDelta(t, 750, tot.Wh, 100, "the counter covers the downtime")
		})
	}
}
