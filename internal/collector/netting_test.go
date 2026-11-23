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

// f32NettingBlocks: an SDM630-like grid block with float32 kWh lifetime counters, whose
// effective resolution grows with the reading (spec 6, input selection).
const f32NettingBlocks = `  grid:
    role: grid
    channels:
      power: { addr: 104, type: i32, unit: W }
      energy_in: { addr: 100, type: f32, unit: kWh, counter: lifetime }
      energy_out: { addr: 102, type: f32, unit: kWh, counter: lifetime }
`

func f32w(v float64) (uint16, uint16) {
	b := math.Float32bits(float32(v))
	return uint16(b >> 16), uint16(b)
}

// perPhaseGrid simulates a meter that does not net its phases: 1 kW net import, while
// 300 W are exported on one phase and imported on another at the same time.
type perPhaseGrid struct{ imp, exp float64 } // kWh

func (g *perPhaseGrid) words(dt time.Duration) map[int]uint16 {
	g.imp += 1.3 * dt.Hours()
	g.exp += 0.3 * dt.Hours()
	ih, il := f32w(g.imp)
	eh, el := f32w(g.exp)
	ph, pl := i32w(1000)
	return map[int]uint16{100: ih, 101: il, 102: eh, 103: el, 104: ph, 105: pl}
}

// TestNettingSwitchesToPowerWhenCountersCoarsen starts method 1 on fine f32 counters;
// once the import reading passes 16384 kWh its float32 step exceeds 1 Wh and the device
// switches to method 2. The net stays exact across the switch, and only method 1 books
// cancelled energy.
func TestNettingSwitchesToPowerWhenCountersCoarsen(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, profHead+f32NettingBlocks, site.DeviceSpec{Unit: 1, Netting: "app"}, "grid")
	g := &perPhaseGrid{imp: 16383.0, exp: 100.0}
	last := moreStart
	src := &fnSource{clock: e.clock, f: func(now time.Time) (map[int]uint16, error) {
		w := g.words(now.Sub(last))
		last = now
		return w, nil
	}}
	c := e.build(t, s, map[string]collector.Source{"d1": src})
	e.runStopped(t, c, moreStart.Add(2*time.Hour))

	from, to := moreStart.Add(-time.Hour), moreStart.Add(3*time.Hour)
	assert.InDelta(t, 2000, e.total(t, "grid", "energy_in", from, to).Wh, 15, "1 kW for 2 h")
	assert.InDelta(t, 0, e.total(t, "grid", "energy_out", from, to).Wh, 1)
	// 1 kWh to the switch at 1.3 kW: about 46 minutes of 300 W cancelled, nothing after.
	cancelled := e.total(t, "grid", "grid_cancelled", from, to).Wh
	assert.InDelta(t, 230, cancelled, 15)
	late := e.total(t, "grid", "grid_cancelled", moreStart.Add(time.Hour), to).Wh
	assert.InDelta(t, 0, late, 0, "method 2 cancels nothing")
}

// TestNettingAcrossOutageBooksCounterNet: with method 2 (coarse counters) only the net of
// the counters is known across an outage; it is booked as import with the gap flag.
func TestNettingAcrossOutageBooksCounterNet(t *testing.T) {
	t.Parallel()
	e := newEnv(t, moreStart)
	s := oneDevice(t, nettingProfile(100), site.DeviceSpec{Unit: 1, Netting: "app"}, "grid")
	outFrom, outTo := moreStart.Add(20*time.Minute), moreStart.Add(50*time.Minute)
	src := &fnSource{clock: e.clock, f: func(now time.Time) (map[int]uint16, error) {
		if !now.Before(outFrom) && now.Before(outTo) {
			return nil, context.DeadlineExceeded
		}
		wh := now.Sub(moreStart).Hours() * 1200 // 1.2 kW import
		imp := uint32(math.Floor(wh/100) * 100)
		return gridWords(imp+500_000, 300_000, 1200), nil
	}}
	c := e.build(t, s, map[string]collector.Source{"d1": src})
	e.runStopped(t, c, moreStart.Add(80*time.Minute))

	from, to := moreStart.Add(-time.Hour), moreStart.Add(2*time.Hour)
	assert.InDelta(t, 1600, e.total(t, "grid", "energy_in", from, to).Wh, 110, "80 min at 1.2 kW")
	gap := e.total(t, "grid", "energy_in", outFrom, outTo)
	assert.InDelta(t, 600, gap.Wh, 110, "the outage's net comes from the counters")
	assert.NotZero(t, gap.Flags&storage.FlagGap, "outage buckets are flagged")
	assert.InDelta(t, 0, e.total(t, "grid", "energy_out", from, to).Wh, 0)
}
