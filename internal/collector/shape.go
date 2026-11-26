package collector

import "time"

// Shape integrates the power of a meter in one direction per slot since the last base of a
// counter. A coarse counter step is then spread like the power flowed instead of evenly
// (spec 5.3). The time between two readings is bridged linearly, also over failed polls and
// outages; the slots of an outage are flagged estimated by the caller.
type Shape struct {
	sign  float64 // +1 or -1: the power sign that flows into the counter
	since time.Time
	integ Integrator
	last  time.Time
	wh    map[int64]float64
}

// NewShape returns an empty shape for power of sign.
func NewShape(sign float64) *Shape { return &Shape{sign: sign, wh: map[int64]float64{}} }

// Add integrates the power p read at at (trapezoid of the part in the shape's direction).
func (s *Shape) Add(p float64, at time.Time) {
	if s.since.IsZero() {
		s.since = at
	}
	prev := s.last
	s.last = at
	wh, ok := s.integ.Update(max(0, s.sign*p), at, false)
	if !ok {
		return
	}
	total := at.Sub(prev).Seconds()
	eachSlot(prev, at, func(slot int64, part time.Duration) {
		s.wh[slot] += wh * part.Seconds() / total
	})
}

// Cut starts a new span at at: the counter took a new base there.
func (s *Shape) Cut(at time.Time) {
	clear(s.wh)
	s.since = at
}

// Weights returns the weight per slot when the shape covers everything since from, else nil.
func (s *Shape) Weights(from time.Time) map[int64]float64 {
	if s.since.IsZero() || s.since.After(from) {
		return nil
	}
	return s.wh
}
