package collector

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/storage"
)

func TestSlots(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 19, 12, 7, 0, 0, time.UTC)
	assert.Equal(t, at.Truncate(15*time.Minute), SlotStart(Slot(at)).UTC())
	// Europe/Berlin DST days have 92 and 100 slots
	for day, want := range map[time.Time]int64{
		time.Date(2026, 3, 29, 0, 0, 0, 0, berlin):  92,
		time.Date(2026, 10, 25, 0, 0, 0, 0, berlin): 100,
		time.Date(2026, 11, 19, 0, 0, 0, 0, berlin): 96,
	} {
		assert.Equal(t, want, Slot(day.AddDate(0, 0, 1))-Slot(day), day)
	}
}

func TestAccumulatorEnergySplit(t *testing.T) {
	t.Parallel()
	a := NewAccumulator()
	slotStart := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	c := 5000.0
	// 30 Wh over 12:59:50 … 13:00:20 (crossing two boundaries is impossible, one here)
	a.AddEnergy(1, Energy{
		Wh: 30, From: slotStart.Add(-10 * time.Second),
		To: slotStart.Add(20 * time.Second), Counter: &c,
	})
	// a gap of 45 minutes: spread evenly over three slots, flagged
	a.AddEnergy(2, Energy{
		Wh: 900, From: slotStart, To: slotStart.Add(45 * time.Minute),
		Flags: storage.FlagEstimated | storage.FlagGap,
	})
	a.AddEnergy(3, Energy{Wh: 7, From: slotStart, To: slotStart})
	a.SetCounter(storage.CounterState{SeriesID: 1, Raw: &c})
	require.False(t, a.Empty())
	b := a.Batch(42)
	assert.Equal(t, int64(42), b.TodayStart)
	s := Slot(slotStart)
	assert.Equal(t, []storage.EnergyBucket{
		{SeriesID: 1, Slot: s - 1, Wh: 10},
		{SeriesID: 1, Slot: s, Wh: 20, Counter: &c},
		{SeriesID: 2, Slot: s, Wh: 300, Flags: 3},
		{SeriesID: 2, Slot: s + 1, Wh: 300, Flags: 3},
		{SeriesID: 2, Slot: s + 2, Wh: 300, Flags: 3},
		{SeriesID: 3, Slot: s, Wh: 7},
	}, b.Energy)
	assert.Len(t, b.Counters, 1)
	assert.True(t, a.Empty(), "batch drains")
}

func TestAccumulatorGaugeAndRestore(t *testing.T) {
	t.Parallel()
	a := NewAccumulator()
	at := time.Date(2026, 11, 19, 12, 1, 0, 0, time.UTC)
	a.AddGauge(1, 100, at, 5*time.Second)
	a.AddGauge(1, 300, at.Add(5*time.Second), 15*time.Second)
	a.AddGauge(2, 1, at, time.Hour)
	b := a.Batch(0)
	require.Len(t, b.Gauges, 2)
	assert.Equal(t, storage.GaugeBucket{
		SeriesID: 1, Slot: Slot(at), Avg: 250, Min: 100,
		Max: 300, Cover: 20,
	}, b.Gauges[0])
	assert.Equal(t, storage.SlotSeconds, b.Gauges[1].Cover, "capped")

	// a failed write is restored under newer values
	c1, c2 := 1.0, 2.0
	a.AddGauge(1, 500, at, 10*time.Second)
	a.AddEnergy(5, Energy{Wh: 1, From: at, To: at})
	a.SetCounter(storage.CounterState{SeriesID: 9, Raw: &c2})
	b.Energy = []storage.EnergyBucket{{SeriesID: 5, Slot: Slot(at), Wh: 2}}
	b.Counters = []storage.CounterState{{SeriesID: 9, Raw: &c1}, {SeriesID: 8, Raw: &c1}}
	a.Restore(b)
	r := a.Batch(0)
	assert.InDelta(t, 3, r.Energy[0].Wh, 0)
	assert.InDelta(t, 2, *r.Counters[1].Raw, 0, "newer counter state wins")
	assert.Len(t, r.Counters, 2)
	assert.Equal(t, 30, r.Gauges[0].Cover)
	assert.InDelta(t, (250*20+500*10)/30.0, r.Gauges[0].Avg, 1e-9)
	assert.InDelta(t, 500, r.Gauges[0].Max, 0)
	assert.Len(t, r.Gauges, 2)
}

// A zero From must not spread over every slot since year 1 (regression: memory blow-up).
func TestAddEnergyZeroFrom(t *testing.T) {
	t.Parallel()
	a := NewAccumulator()
	to := time.Date(2026, 11, 19, 12, 7, 0, 0, time.UTC)
	a.AddEnergy(1, Energy{Wh: 5, To: to})
	b := a.Batch(0)
	require.Len(t, b.Energy, 1)
	assert.Equal(t, Slot(to), b.Energy[0].Slot)
	assert.InDelta(t, 5, b.Energy[0].Wh, 1e-9)
}
