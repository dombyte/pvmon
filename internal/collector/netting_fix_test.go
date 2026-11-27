package collector_test

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/site"
)

// registerStepProfile has Solis-like u32 0.01 kWh counters without a resolution: one raw
// count is 10 Wh.
const registerStepProfile = profHead + `  grid:
    role: grid
    channels:
      power: { addr: 104, type: i32, unit: W }
      energy_in: { addr: 100, type: u32, scale: 0.01, unit: kWh, counter: lifetime }
      energy_out: { addr: 102, type: u32, scale: 0.01, unit: kWh, counter: lifetime }
`

// TestNettingCountsRegisterStepAsResolution: counters with 10 Wh raw steps are too coarse
// to net per poll, so the power is integrated and nothing is booked in both directions.
func TestNettingCountsRegisterStepAsResolution(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, registerStepProfile, site.DeviceSpec{Unit: 1, Netting: "app"}, "grid")
	src := &fnSource{clock: e.clock, f: func(now time.Time) (map[int]uint16, error) {
		// 1.2 kW net import; 3 kW more are counted in both directions (per phase).
		h := now.Sub(moreStart).Hours()
		return gridWords(uint32(math.Floor(h*4200/10))+500, uint32(math.Floor(h*3000/10))+300,
			1200), nil
	}}
	e.runStopped(t, e.build(t, s, map[string]collector.Source{"d1": src}),
		moreStart.Add(time.Hour))
	from, to := moreStart.Add(-time.Hour), moreStart.Add(2*time.Hour)
	assert.InDelta(t, 1200, e.total(t, "grid", "energy_in", from, to).Wh, 5)
	assert.InDelta(t, 0, e.total(t, "grid", "energy_out", from, to).Wh, 5)
}

// TestNettingNearZeroBooksNoRounding: import and export of a per-phase meter grow alike
// and step in different polls. The net is 0, so neither direction gets energy; a real net
// still arrives in full, at most the deadband late.
func TestNettingNearZeroBooksNoRounding(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, nettingProfile(1), site.DeviceSpec{Unit: 1, Netting: "app"}, "grid")
	src := &fnSource{clock: e.clock, f: func(now time.Time) (map[int]uint16, error) {
		// 300 W counted in both directions, export half a step behind; then 600 W import.
		h := now.Sub(moreStart).Hours()
		imp := 300*h + 600*max(0, h-1)
		return gridWords(uint32(imp)+500, uint32(300*h+0.5)+300, 0), nil
	}}
	e.runStopped(t, e.build(t, s, map[string]collector.Source{"d1": src}),
		moreStart.Add(2*time.Hour))
	idleTo := moreStart.Add(time.Hour)
	for _, ch := range []string{"energy_in", "energy_out"} {
		assert.Zero(t, e.total(t, "grid", ch, moreStart, idleTo).Wh, ch)
	}
	end := moreStart.Add(3 * time.Hour)
	assert.InDelta(t, 600, e.total(t, "grid", "energy_in", idleTo, end).Wh, 3)
	assert.Zero(t, e.total(t, "grid", "energy_out", idleTo, end).Wh)
}
