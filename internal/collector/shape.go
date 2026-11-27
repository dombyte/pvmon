package collector

import (
	"maps"
	"time"
)

// Shape integrates the power of a meter in one direction per slot since the last base of a
// counter. A coarse counter step is then spread like the power flowed instead of evenly
// (spec 5.3). The time between two readings is bridged linearly, also over failed polls.
// An outage is not bridged: its power is unknown.
type Shape struct {
	sign  float64 // +1 or -1: the power sign that flows into the counter
	since time.Time
	integ Integrator
	last  time.Time
	wh    map[int64]float64
	lost  []span // outages since the base
	prev  mark   // the reading before the last one
	pin   mark   // where a reset booking may start
}

// mark is the weight that the slot of a reading held up to that reading.
type mark struct {
	at time.Time
	wh float64
}

// span is a time without power readings.
type span struct{ from, to time.Time }

// NewShape returns an empty shape for power of sign.
func NewShape(sign float64) *Shape { return &Shape{sign: sign, wh: map[int64]float64{}} }

// Add integrates the power p read at at (trapezoid of the part in the shape's direction).
func (s *Shape) Add(p float64, at time.Time) {
	if s.since.IsZero() {
		s.since = at
	}
	prev := s.last
	s.prev = mark{prev, s.wh[Slot(prev)]}
	s.last = at
	wh, ok := s.integ.Update(max(0, s.sign*p), at, false)
	if !ok || wh == 0 {
		return // no entry for no flow: an idle counter keeps an empty shape
	}
	total := at.Sub(prev).Seconds()
	eachSlot(prev, at, func(slot int64, part time.Duration) {
		s.wh[slot] += wh * part.Seconds() / total
	})
}

// Lose notes that the time up to at was an outage: the next reading starts anew.
func (s *Shape) Lose(at time.Time) {
	if s.since.IsZero() {
		return
	}
	s.lost = append(s.lost, span{s.last, at})
	s.integ.Reset()
}

// Pin remembers the weight at the reading before the last one: the last one showed a
// counter drop, so a reset booking starts there.
func (s *Shape) Pin() { s.pin = s.prev }

// Cut starts a new span at at: the counter took a new base there.
func (s *Shape) Cut(at time.Time) {
	clear(s.wh)
	s.since, s.lost, s.pin = at, nil, mark{}
}

// covers reports whether the shape has seen all the time since from.
func (s *Shape) covers(from time.Time) bool {
	return !s.since.IsZero() && !s.since.After(from)
}

// Weights returns the weight per slot of the time since from for a delta of total Wh when
// the shape covers all of that time, else nil. Energy that the measured power does not
// explain flowed in the outages, evenly over their time. The first slot counts only with
// its part after from: after a counter reset the power before it belongs to the previous
// period. That part is exact when from is pinned.
func (s *Shape) Weights(from time.Time, total float64) map[int64]float64 {
	if !s.covers(from) {
		return nil
	}
	w := maps.Clone(s.wh)
	first := Slot(from)
	if x, ok := w[first]; ok {
		w[first] = s.after(from, x)
	}
	s.addLost(w, from, total)
	return w
}

// after is the part of x, the weight of the slot of from, that flowed after from.
func (s *Shape) after(from time.Time, x float64) float64 {
	if from.Equal(s.pin.at) {
		return x - s.pin.wh
	}
	// shortcut: without a pin (a reset placed in an outage or an earlier run) the power is
	// taken as even over the covered part of the slot; nothing saw it at that instant.
	first := Slot(from)
	end := SlotStart(first).Add(slotLen)
	covered := end.Sub(maxTime(s.since, SlotStart(first)))
	return x * end.Sub(from).Seconds() / covered.Seconds()
}

// addLost spreads the part of total above the measured weights since from over the outages.
func (s *Shape) addLost(w map[int64]float64, from time.Time, total float64) {
	for slot, x := range w {
		if slot >= Slot(from) {
			total -= x
		}
	}
	var length time.Duration
	for _, l := range s.lost {
		length += max(0, l.to.Sub(maxTime(l.from, from)))
	}
	if total <= 0 || length == 0 {
		return
	}
	for _, l := range s.lost {
		eachSlot(maxTime(l.from, from), l.to, func(slot int64, part time.Duration) {
			w[slot] += total * part.Seconds() / length.Seconds()
		})
	}
}
