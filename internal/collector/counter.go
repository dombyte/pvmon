package collector

import (
	"math"
	"time"
)

// Delta is the energy a counter or integrator books for one poll.
type Delta struct {
	Wh        float64
	Gap       bool      // the poll followed a gap; spread over the gap's buckets
	ResetSeen bool      // a daily reset was booked
	Held      bool      // the reading was held (glitch guard or waiting for the reset group)
	Event     string    // one-time events for the log: replaced, jump, overflow
	From      time.Time // a confirmed reset: book from the poll before the drop
}

// Counter events.
const (
	EventReplaced = "replaced" // clear decrease: meter replaced, new base
	EventJump     = "jump"     // implausible jump confirmed by the next reading: new base
	EventOverflow = "overflow" // u32 wrap-around
)

// CounterConfig describes one counter input.
type CounterConfig struct {
	Resolution float64 // profile resolution in Wh (0 = none)
	Step       float64 // Wh of one raw count of an integer register (0 = none)
	MaxPower   float64 // W; 0 disables the glitch guard
	Wrap       float64 // Wh value at which the raw counter wraps (u32); 0 = never
	Float32    bool    // raw value is an f32 in Unit; precision limits the resolution
	UnitFactor float64 // Wh per raw unit (kWh: 1000)
}

// EffectiveResolution = max(profile resolution, type precision at v) (spec 5.3).
func (c CounterConfig) EffectiveResolution(v float64) float64 {
	res := c.Resolution
	if c.Float32 {
		f := c.UnitFactor
		if f == 0 {
			f = 1
		}
		x := float32(math.Abs(v) / f)
		ulp := float64(math.Nextafter32(x, math.MaxFloat32)-x) * f
		res = math.Max(res, ulp)
	}
	return res
}

// plausible reports whether d Wh can have been produced within elapsed at max power. A
// counter counts in steps: the energy below one step at the previous reading shows up
// later, so a delta may exceed max power × elapsed by one step (resolution or raw count).
func (c CounterConfig) plausible(d float64, elapsed time.Duration, v float64) bool {
	return c.MaxPower == 0 ||
		d <= c.MaxPower*elapsed.Hours()+math.Max(c.EffectiveResolution(v), c.Step)
}

// reading is one accepted or held raw value.
type reading struct {
	v  float64
	at time.Time
}

// Lifetime tracks a monotonic counter (spec 5.3).
type Lifetime struct {
	cfg  CounterConfig
	base *reading
	held *reading
	seen time.Time // last reading that was not held: the glitch guard counts from there
}

// NewLifetime returns a tracker; base restores the persisted raw value (nil = none).
func NewLifetime(cfg CounterConfig, base *float64, at time.Time) *Lifetime {
	l := &Lifetime{cfg: cfg, seen: at}
	if base != nil {
		l.base = &reading{v: *base, at: at}
	}
	return l
}

// Base returns the current base value and its time (nil before the first reading).
func (l *Lifetime) Base() (*float64, time.Time) {
	if l.base == nil {
		return nil, time.Time{}
	}
	v := l.base.v
	return &v, l.base.at
}

// Update books the reading v at at.
func (l *Lifetime) Update(v float64, at time.Time, gap bool) Delta {
	d := l.update(v, at, gap)
	if !d.Held {
		l.seen = at
	}
	return d
}

func (l *Lifetime) update(v float64, at time.Time, gap bool) Delta {
	if l.base == nil {
		l.base = &reading{v, at}
		return Delta{}
	}
	// Since the last read, not the last change: an idle counter keeps its base for days.
	elapsed := at.Sub(l.seen)
	d := v - l.base.v
	event := ""
	if d < 0 && l.cfg.Wrap > 0 && l.cfg.plausible(d+l.cfg.Wrap, elapsed, v) {
		d += l.cfg.Wrap
		event = EventOverflow
	}
	if d <= 0 {
		return l.decrease(v, at, d)
	}
	if !l.cfg.plausible(d, elapsed, v) {
		return l.hold(v, at)
	}
	l.base, l.held = &reading{v, at}, nil
	return Delta{Wh: d, Gap: gap, Event: event}
}

// decrease handles an unchanged or lower reading. Jitter within the resolution keeps the
// base, so the next step is spread from the last change, as for daily counters (spec 5.3),
// and drops a held glitch. A clear decrease means the meter was replaced (new base, no
// energy).
func (l *Lifetime) decrease(v float64, at time.Time, d float64) Delta {
	if -d <= l.cfg.EffectiveResolution(l.base.v) {
		l.held = nil
		return Delta{}
	}
	l.base, l.held = &reading{v, at}, nil
	return Delta{Event: EventReplaced}
}

// hold keeps an implausible jump out of the books. When the next reading is consistent
// with the held one, the jump is real: it becomes the new base without energy.
func (l *Lifetime) hold(v float64, at time.Time) Delta {
	if h := l.held; h != nil && v >= h.v && l.cfg.plausible(v-h.v, at.Sub(h.at), v) {
		l.base, l.held = &reading{v, at}, nil
		return Delta{Event: EventJump}
	}
	l.held = &reading{v, at}
	return Delta{Held: true}
}

// Integrator integrates power between two consecutive successful polls (trapezoid).
type Integrator struct {
	prev *reading
}

// Update returns the signed energy in Wh since the previous poll; ok is false when there
// is no previous poll or the poll followed a gap (the gap stays a gap).
func (i *Integrator) Update(p float64, at time.Time, gap bool) (wh float64, ok bool) {
	prev := i.prev
	i.prev = &reading{p, at}
	if prev == nil || gap || !at.After(prev.at) {
		return 0, false
	}
	return (prev.v + p) / 2 * at.Sub(prev.at).Hours(), true
}

// Reset forgets the previous poll (after a failed poll).
func (i *Integrator) Reset() { i.prev = nil }
