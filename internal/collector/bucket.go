package collector

import (
	"math"
	"sort"
	"time"

	"github.com/dombyte/pvmon/internal/storage"
)

const slotLen = storage.SlotSeconds * time.Second

// Slot returns the 15-minute UTC slot of t (spec 5.2).
func Slot(t time.Time) int64 { return t.Unix() / storage.SlotSeconds }

// SlotStart returns the start time of a slot.
func SlotStart(slot int64) time.Time { return time.Unix(slot*storage.SlotSeconds, 0) }

type gaugeAcc struct {
	sum, min, max float64
	cover         time.Duration
}

type slotKey struct {
	series int64
	slot   int64
}

// Accumulator collects the open buckets of one collector between flushes. Gauges are
// partial aggregates, energy is the Wh added since the last flush; storage merges both.
// It is owned by the collector goroutine.
type Accumulator struct {
	gauges   map[slotKey]*gaugeAcc
	energy   map[slotKey]*storage.EnergyBucket
	counters map[int64]storage.CounterState
}

// NewAccumulator returns an empty accumulator.
func NewAccumulator() *Accumulator {
	return &Accumulator{
		gauges:   map[slotKey]*gaugeAcc{},
		energy:   map[slotKey]*storage.EnergyBucket{},
		counters: map[int64]storage.CounterState{},
	}
}

// AddGauge adds a sample valid for weight (the device interval) to the slot of at.
func (a *Accumulator) AddGauge(series int64, v float64, at time.Time, weight time.Duration) {
	k := slotKey{series, Slot(at)}
	g, ok := a.gauges[k]
	if !ok {
		g = &gaugeAcc{min: v, max: v}
		a.gauges[k] = g
	}
	g.sum += v * weight.Seconds()
	g.cover += weight
	g.min, g.max = math.Min(g.min, v), math.Max(g.max, v)
}

// Energy is energy produced in (From, To] with its bucket flags and the raw counter at To.
type Energy struct {
	Wh       float64
	From, To time.Time
	Flags    int
	Counter  *float64
}

// AddEnergy books e proportionally to the time each slot overlaps (From, To]; From == To
// books everything into the slot of To.
func (a *Accumulator) AddEnergy(series int64, e Energy) {
	wh, from, to, flags, counter := e.Wh, e.From, e.To, e.Flags, e.Counter
	// A zero From would spread over every slot since year 1 and exhaust memory.
	if from.IsZero() || !to.After(from) {
		a.add(slotKey{series, Slot(to)}, wh, flags, counter)
		return
	}
	total := to.Sub(from)
	for slot := Slot(from); slot <= Slot(to); slot++ {
		start, end := maxTime(from, SlotStart(slot)), minTime(to, SlotStart(slot).Add(slotLen))
		if !end.After(start) {
			continue
		}
		var c *float64
		if slot == Slot(to) {
			c = counter
		}
		a.add(slotKey{series, slot}, wh*end.Sub(start).Seconds()/total.Seconds(), flags, c)
	}
}

func (a *Accumulator) add(k slotKey, wh float64, flags int, counter *float64) {
	e, ok := a.energy[k]
	if !ok {
		e = &storage.EnergyBucket{SeriesID: k.series, Slot: k.slot}
		a.energy[k] = e
	}
	e.Wh += wh
	e.Flags |= flags
	if counter != nil {
		e.Counter = counter
	}
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// SetCounter records the counter state to persist with the next flush.
func (a *Accumulator) SetCounter(st storage.CounterState) { a.counters[st.SeriesID] = st }

// Empty reports whether nothing is pending.
func (a *Accumulator) Empty() bool {
	return len(a.gauges) == 0 && len(a.energy) == 0 && len(a.counters) == 0
}

// Batch drains everything pending into one storage batch, sorted for stable writes.
func (a *Accumulator) Batch(todayStart int64) storage.Batch {
	b := storage.Batch{TodayStart: todayStart}
	for k, g := range a.gauges {
		cover := int(g.cover.Seconds())
		b.Gauges = append(b.Gauges, storage.GaugeBucket{
			SeriesID: k.series, Slot: k.slot, Avg: g.sum / g.cover.Seconds(),
			Min: g.min, Max: g.max, Cover: min(cover, storage.SlotSeconds),
		})
	}
	for _, e := range a.energy {
		b.Energy = append(b.Energy, *e)
	}
	for _, c := range a.counters {
		b.Counters = append(b.Counters, c)
	}
	sort.Slice(b.Gauges, func(i, j int) bool {
		return less(b.Gauges[i].SeriesID,
			b.Gauges[i].Slot, b.Gauges[j].SeriesID, b.Gauges[j].Slot)
	})
	sort.Slice(b.Energy, func(i, j int) bool {
		return less(b.Energy[i].SeriesID,
			b.Energy[i].Slot, b.Energy[j].SeriesID, b.Energy[j].Slot)
	})
	sort.Slice(b.Counters, func(i, j int) bool {
		return b.Counters[i].SeriesID <
			b.Counters[j].SeriesID
	})
	clear(a.gauges)
	clear(a.energy)
	clear(a.counters)
	return b
}

func less(s1, t1, s2, t2 int64) bool {
	if s1 != s2 {
		return s1 < s2
	}
	return t1 < t2
}

// Restore puts a batch that failed to write back, so no energy is lost; newer pending
// values are kept on top of it.
func (a *Accumulator) Restore(b storage.Batch) {
	for _, e := range b.Energy {
		a.add(slotKey{e.SeriesID, e.Slot}, e.Wh, e.Flags, e.Counter)
	}
	for _, g := range b.Gauges {
		k := slotKey{g.SeriesID, g.Slot}
		w := time.Duration(g.Cover) * time.Second
		cur, ok := a.gauges[k]
		if !ok {
			a.gauges[k] = &gaugeAcc{sum: g.Avg * w.Seconds(), min: g.Min, max: g.Max, cover: w}
			continue
		}
		cur.sum += g.Avg * w.Seconds()
		cur.cover += w
		cur.min, cur.max = math.Min(cur.min, g.Min), math.Max(cur.max, g.Max)
	}
	for _, c := range b.Counters {
		if _, newer := a.counters[c.SeriesID]; !newer {
			a.counters[c.SeriesID] = c
		}
	}
}
