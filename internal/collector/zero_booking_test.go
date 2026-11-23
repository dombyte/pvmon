package collector_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/site"
)

// minBuckets is the number of 15-minute slots a one-hour run must have covered at least.
const minBuckets = 4

func zeroRange() (time.Time, time.Time) {
	return moreStart.Add(-time.Hour), moreStart.Add(2 * time.Hour)
}

func TestUnchangedCounterBooksZeroBuckets(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, nettingProfile(1), site.DeviceSpec{Unit: 1}, "grid")
	src := &fnSource{clock: e.clock, f: func(time.Time) (map[int]uint16, error) {
		return gridWords(1000, 500, 0), nil // counters never move
	}}
	e.runStopped(t, e.build(t, s, map[string]collector.Source{"d1": src}),
		moreStart.Add(time.Hour))
	from, to := zeroRange()
	for _, ch := range []string{"energy_in", "energy_out"} {
		tot := e.total(t, "grid", ch, from, to)
		assert.Zero(t, tot.Wh, ch)
		assert.GreaterOrEqual(t, tot.Buckets, minBuckets, ch+": 0 Wh slots are booked")
	}
}

func TestNettingBooksZeroInOtherDirection(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ di, de uint32 }{
		"import only": {di: 3, de: 0},
		"export only": {di: 0, de: 3},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, moreStart)
			s := oneDevice(t, nettingProfile(1), site.DeviceSpec{Unit: 1, Netting: "app"},
				"grid")
			var imp, exp uint32
			src := &fnSource{clock: e.clock, f: func(time.Time) (map[int]uint16, error) {
				imp, exp = imp+c.di, exp+c.de
				return gridWords(imp, exp, 0), nil
			}}
			e.runStopped(t, e.build(t, s, map[string]collector.Source{"d1": src}),
				moreStart.Add(time.Hour))
			from, to := zeroRange()
			in := e.total(t, "grid", "energy_in", from, to)
			out := e.total(t, "grid", "energy_out", from, to)
			assert.GreaterOrEqual(t, in.Buckets, minBuckets)
			assert.Equal(t, in.Buckets, out.Buckets, "both directions cover the same slots")
			if c.di > 0 {
				assert.Positive(t, in.Wh)
				assert.Zero(t, out.Wh)
			} else {
				assert.Zero(t, in.Wh)
				assert.Positive(t, out.Wh)
			}
		})
	}
}

func TestNettingSkippedWithInvalidCounter(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, nettingProfile(1), site.DeviceSpec{Unit: 1, Netting: "app"}, "grid")
	var imp uint32
	src := &fnSource{clock: e.clock, f: func(time.Time) (map[int]uint16, error) {
		imp += 3
		w := gridWords(imp, 0, 0)
		delete(w, 102) // the export counter is never readable, also not on the first poll
		delete(w, 103)
		return w, nil
	}}
	e.runStopped(t, e.build(t, s, map[string]collector.Source{"d1": src}),
		moreStart.Add(time.Hour))
	from, to := zeroRange()
	for _, ch := range []string{"energy_in", "energy_out"} {
		assert.Zero(t, e.total(t, "grid", ch, from, to).Buckets, ch)
	}
}

func TestNettingResumesAfterInvalidPoll(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, nettingProfile(1), site.DeviceSpec{Unit: 1, Netting: "app"}, "grid")
	var imp uint32
	src := &fnSource{clock: e.clock, f: func(now time.Time) (map[int]uint16, error) {
		imp += 3
		w := gridWords(imp, 0, 0)
		if d := now.Sub(moreStart); d < 10*time.Minute {
			delete(w, 100) // import counter invalid at the beginning
			delete(w, 101)
		}
		return w, nil
	}}
	e.runStopped(t, e.build(t, s, map[string]collector.Source{"d1": src}),
		moreStart.Add(time.Hour))
	from, to := zeroRange()
	in := e.total(t, "grid", "energy_in", from, to)
	require.Positive(t, in.Buckets)
	assert.Less(t, in.Wh, float64(imp), "no energy is booked for the unread start")
	assert.Equal(t, in.Buckets, e.total(t, "grid", "energy_out", from, to).Buckets)
}

func TestIntegratedMeterBooksZeroInOtherDirection(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		power  int
		in, ou bool
	}{
		"charging":    {power: 1, ou: true},
		"discharging": {power: -1, in: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, moreStart)
			s := oneDevice(t, integProfile("battery"), site.DeviceSpec{Unit: 1}, "main", "grid")
			src := &fnSource{clock: e.clock, f: func(time.Time) (map[int]uint16, error) {
				return map[int]uint16{100: i16w(c.power)}, nil
			}}
			e.runStopped(t, e.build(t, s, map[string]collector.Source{"d1": src}),
				moreStart.Add(time.Hour))
			from, to := zeroRange()
			in := e.total(t, "main", "energy_in", from, to)
			out := e.total(t, "main", "energy_out", from, to)
			assert.GreaterOrEqual(t, in.Buckets, minBuckets)
			assert.Equal(t, in.Buckets, out.Buckets)
			flowing, idle := in, out
			if c.ou {
				flowing, idle = out, in
			}
			assert.Positive(t, flowing.Wh)
			assert.Zero(t, idle.Wh)
		})
	}
}
