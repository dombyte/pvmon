package collector

import (
	"slices"
	"time"
)

// Reset detection (spec 5.3). A reset is a data event, never triggered by time: device
// clocks drift and often lack NTP and DST. A drop counts once it persisted for ResetConfirm;
// the counter itself buffers the energy meanwhile, so nothing is lost by waiting.
const (
	// ResetConfirm is how long a drop must persist before it is booked as a reset.
	ResetConfirm = 15 * time.Minute
	// ResetOverdue is how long after the last reset a missing reset is reported.
	ResetOverdue = 25 * time.Hour
)

// Reset events, logged once per occurrence.
const (
	EventResetPartial = "reset of only part of the reset group (register stuck?)"
	EventResetMissing = "no reset for more than 25 h"
)

// Daily is one daily_reset counter of a reset group.
type Daily struct {
	cfg  CounterConfig
	base *reading
	peak float64  // highest value held while waiting for the group
	held *reading // implausible jump waiting for confirmation
}

// NewDaily returns a member; base restores the persisted raw value (nil = none).
func NewDaily(cfg CounterConfig, base *float64, at time.Time) *Daily {
	d := &Daily{cfg: cfg}
	if base != nil {
		d.base = &reading{v: *base, at: at}
	}
	return d
}

// Base returns the current base value and its time (nil before the first reading).
func (d *Daily) Base() (*float64, time.Time) {
	if d.base == nil {
		return nil, time.Time{}
	}
	v := d.base.v
	return &v, d.base.at
}

// Group is a reset group: all its counters reset together once per device day.
type Group struct {
	members   []*Daily
	rollover  time.Duration // configured local reset time (only before the first reset)
	loc       *time.Location
	last      time.Time // previous poll
	lastReset time.Time // last confirmed reset (zero = none seen yet)
	pending   *pendingReset
	warned    time.Time // lastReset for which a missing reset was reported
}

// pendingReset is a drop waiting for confirmation.
type pendingReset struct {
	at   time.Time // first poll showing the drop
	prev time.Time // poll before it: the reset happened in (prev, at]
	gap  bool
}

// NewGroup builds a group. last is the last poll and lastReset the last confirmed reset as
// persisted (zero if unknown); rollover is the configured local reset time.
func NewGroup(members []*Daily, rollover time.Duration, loc *time.Location,
	last, lastReset time.Time,
) *Group {
	return &Group{
		members: members, rollover: rollover, loc: loc, last: last, lastReset: lastReset,
	}
}

// LastReset returns the instant of the last confirmed reset (zero if none).
func (g *Group) LastReset() time.Time { return g.lastReset }

// Reading is one member value of a poll; Valid is false when the value is missing.
type Reading struct {
	Value float64
	Valid bool
}

// Update books one poll of all members (same order as members).
func (g *Group) Update(at time.Time, gap bool, readings []Reading) []Delta {
	stuck, dropped := g.drops(readings)
	confirmed := g.track(at, gap, dropped)
	out := g.book(at, gap, readings, stuck, confirmed)
	if confirmed != nil {
		g.lastReset = confirmed.at
		g.report(out, slices.Contains(stuck, true), EventResetPartial)
	}
	g.report(out, g.overdue(at), EventResetMissing)
	g.last = at
	return out
}

// book updates every member; on a confirmed reset all but the stuck members reset.
func (g *Group) book(at time.Time, gap bool, readings []Reading, stuck []bool,
	confirmed *pendingReset,
) []Delta {
	out := make([]Delta, len(g.members))
	var from time.Time
	if confirmed != nil {
		// A confirmation releases what was held since the drop: after a gap, every
		// member's booking spans it. The start is taken before members get new bases.
		gap = gap || confirmed.gap
		from = g.resetFrom(confirmed)
	}
	for i, m := range g.members {
		if !readings[i].Valid {
			continue
		}
		if confirmed == nil || stuck[i] {
			out[i] = m.update(readings[i].Value, at, gap, false, g.pending != nil)
			continue
		}
		out[i] = m.update(readings[i].Value, at, gap, true, false)
		out[i].From = from
	}
	return out
}

// resetFrom is where a confirmed reset books its new energy from. Without a gap the reset
// happened between the poll before the drop and the drop. After a gap that window can span
// hours or a restart (the poll before the drop may already have shown it), so the reset is
// placed at its expected instant, but never before the last reading before the drop: the
// energy since the reset is not spread back into the previous day.
func (g *Group) resetFrom(p *pendingReset) time.Time {
	if !p.gap {
		return p.prev
	}
	floor := time.Time{}
	for _, m := range g.members {
		if m.base != nil {
			floor = maxTime(floor, m.base.at)
		}
	}
	if r := g.expectedReset(p.at); r.After(floor) {
		return r
	}
	return floor
}

// drops classifies the members: stuck = could show a reset (value above its resolution)
// but did not drop; dropped = at least one member dropped.
func (g *Group) drops(readings []Reading) ([]bool, bool) {
	stuck, dropped := make([]bool, len(g.members)), false
	for i, m := range g.members {
		if !readings[i].Valid || m.base == nil {
			continue
		}
		if m.dropped(readings[i].Value) {
			dropped = true
		} else if m.base.v > m.cfg.EffectiveResolution(m.base.v) {
			stuck[i] = true
		}
	}
	return stuck, dropped
}

// track follows a drop: the first poll showing one opens a pending reset, a drop that
// vanishes was a glitch, and one that persisted for ResetConfirm is returned as confirmed.
func (g *Group) track(at time.Time, gap, drop bool) *pendingReset {
	switch {
	case !drop:
		g.pending = nil
	case g.pending == nil:
		g.pending = &pendingReset{at: at, prev: g.last, gap: gap}
	case at.Sub(g.pending.at) >= ResetConfirm:
		p := g.pending
		g.pending = nil
		return p
	}
	return nil
}

// overdue reports, once per reset, that no reset followed within ResetOverdue.
func (g *Group) overdue(at time.Time) bool {
	if g.lastReset.IsZero() || g.pending != nil || g.warned.Equal(g.lastReset) ||
		at.Sub(g.lastReset) <= ResetOverdue {
		return false
	}
	g.warned = g.lastReset
	return true
}

// report attaches an event to the first member's delta.
func (g *Group) report(out []Delta, cond bool, event string) {
	if cond && len(out) > 0 {
		out[0].Event = event
	}
}

// resetTime returns the configured rollover instant on the local day of at. The rollover
// is a wall clock time, so it is built with time.Date (DST days have 23 or 25 hours).
func (g *Group) resetTime(at time.Time) time.Time {
	l := at.In(g.loc)
	h, m := int(g.rollover/time.Hour), int(g.rollover%time.Hour/time.Minute)
	return time.Date(l.Year(), l.Month(), l.Day(), h, m, 0, 0, g.loc)
}

// expectedReset returns the latest expected reset instant at or before at: the wall clock
// time of the last confirmed reset (learned, spec 5.3), or the configured rollover.
func (g *Group) expectedReset(at time.Time) time.Time {
	if g.lastReset.IsZero() {
		r := g.resetTime(at)
		if r.After(at) {
			r = g.resetTime(at.AddDate(0, 0, -1))
		}
		return r
	}
	h, m, s := g.lastReset.In(g.loc).Clock()
	l := at.In(g.loc)
	r := time.Date(l.Year(), l.Month(), l.Day(), h, m, s, 0, g.loc)
	if r.After(at) {
		r = r.AddDate(0, 0, -1)
	}
	return r
}

// PeriodStart returns the start of the device day containing at: the last confirmed reset,
// or before the first one the configured rollover. Energy shown by a daily register belongs
// to the period since then and is never booked before it.
func (g *Group) PeriodStart(at time.Time) time.Time {
	if !g.lastReset.IsZero() && !g.lastReset.After(at) {
		return g.lastReset
	}
	r := g.resetTime(at)
	if r.After(at) {
		r = g.resetTime(at.AddDate(0, 0, -1))
	}
	return r
}

func (d *Daily) dropped(v float64) bool {
	return d.base.v-v > d.cfg.EffectiveResolution(d.base.v)
}

func (d *Daily) update(v float64, at time.Time, gap, reset, awaiting bool) Delta {
	if d.base == nil {
		d.base = &reading{v, at}
		return Delta{}
	}
	if reset {
		return d.afterReset(v, at, gap)
	}
	diff := v - d.base.v
	switch {
	case d.dropped(v):
		return Delta{Held: true} // drop not confirmed by the group
	case diff <= 0:
		return Delta{} // jitter; keep the base
	case !d.cfg.plausible(diff, at.Sub(d.base.at), v):
		return d.hold(v, at)
	case awaiting:
		d.peak = max(d.peak, v)
		return Delta{Held: true}
	}
	d.base, d.peak, d.held = &reading{v, at}, 0, nil
	return Delta{Wh: diff, Gap: gap}
}

// hold keeps an implausible jump out of the books; a next reading consistent with it
// makes it the new base without energy (e.g. after a transient all-zero read).
func (d *Daily) hold(v float64, at time.Time) Delta {
	if h := d.held; h != nil && v >= h.v && d.cfg.plausible(v-h.v, at.Sub(h.at), v) {
		d.base, d.peak, d.held = &reading{v, at}, 0, nil
		return Delta{Event: EventJump}
	}
	d.held = &reading{v, at}
	return Delta{Held: true}
}

// afterReset books a confirmed group reset: a member that dropped books its held
// pre-reset rise plus its new value; a member that could not show the reset books only
// its new value, so nothing is counted twice.
func (d *Daily) afterReset(v float64, at time.Time, gap bool) Delta {
	wh := v
	if d.dropped(v) && d.peak > d.base.v {
		wh += d.peak - d.base.v
	}
	d.base, d.peak, d.held = &reading{v, at}, 0, nil
	return Delta{Wh: wh, Gap: gap, ResetSeen: true}
}
