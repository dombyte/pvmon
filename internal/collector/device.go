package collector

import (
	"context"
	"time"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
)

// Source reads one raw poll of a device (Fetcher).
type Source interface {
	Fetch(ctx context.Context) (profile.Sample, error)
}

// GapAfter: a poll more than the interval plus this after the last successful one is a
// gap poll (app not running, clock jump > 5 min; spec 5.2, 5.3).
const GapAfter = 5 * time.Minute

// deviceRun is the runtime state of one device, owned by the collector goroutine.
type deviceRun struct {
	dev     *site.Device
	plan    *devicePlan
	src     Source
	ids     map[storage.SeriesKey]int64
	life    map[ref]*Lifetime
	groups  map[string]*groupRun
	integ   map[ref]*Integrator
	shapes  map[ref]*Shape    // power shape per counter with a power channel
	gapFrom map[ref]time.Time // start of the first outage since the counter's base
	booked  map[ref]time.Time // last poll that booked a delta of the counter
	netInt  Integrator
	netMode string
	status  map[string]int64 // last stored code per status register
	conn    int64            // last stored connection code, -1 none
	logged  map[string]bool
	lastOK  time.Time
	failed  bool
	next    time.Time
	state   string
}

type groupRun struct {
	g    *Group
	refs []ref
}

// pollResult is what a poll produced besides the accumulator.
type pollResult struct {
	statusEvents []storage.StatusEvent
	logs         []string
}

// runInput is what a device run is restored from.
type runInput struct {
	dev      *site.Device
	plan     *devicePlan
	src      Source
	ids      map[storage.SeriesKey]int64
	states   map[int64]storage.CounterState
	loc      *time.Location
	rollover time.Duration
}

// newDeviceRun restores trackers from persisted counter states.
func newDeviceRun(in runInput) *deviceRun {
	d, pl, ids, states := in.dev, in.plan, in.ids, in.states
	loc, rollover, src := in.loc, in.rollover, in.src
	r := &deviceRun{
		dev: d, plan: pl, src: src, ids: ids, life: map[ref]*Lifetime{},
		groups: map[string]*groupRun{}, integ: map[ref]*Integrator{},
		shapes: map[ref]*Shape{}, gapFrom: map[ref]time.Time{}, booked: map[ref]time.Time{},
		status: map[string]int64{}, conn: -1, logged: map[string]bool{},
	}
	for _, cp := range pl.counters {
		r.restoreCounter(cp, states[ids[cp.input]], loc, rollover)
	}
	for _, ip := range pl.integrates {
		r.integ[ip.power] = &Integrator{}
	}
	return r
}

// restoreCounter restores the tracker, outage start and shape of one counter input. A
// missing state is the zero value.
func (r *deviceRun) restoreCounter(cp counterPlan, st storage.CounterState,
	loc *time.Location, rollover time.Duration,
) {
	rs := counterRestore{}
	if st.Raw != nil {
		rs.base, rs.at, rs.seen = st.Raw, time.UnixMilli(st.At), time.UnixMilli(st.Seen)
		// The last read, not the last change: a coarse counter may not move for hours.
		r.lastOK = maxTime(r.lastOK, maxTime(rs.at, rs.seen))
	}
	if st.ResetAt > 0 {
		rs.resetAt = time.UnixMilli(st.ResetAt)
	}
	if st.GapFrom > 0 {
		r.gapFrom[cp.ref] = time.UnixMilli(st.GapFrom)
	}
	if cp.sign != 0 {
		r.shapes[cp.ref] = NewShape(cp.sign)
	}
	r.addCounter(cp, rs, loc, rollover)
}

// counterRestore is the persisted state of one counter input.
type counterRestore struct {
	base    *float64
	at      time.Time // when base was read
	seen    time.Time // last poll that read the counter
	resetAt time.Time // last confirmed reset of its group (daily_reset only)
}

func (r *deviceRun) addCounter(cp counterPlan, st counterRestore,
	loc *time.Location, rollover time.Duration,
) {
	if cp.kind == profile.CounterLifetime {
		// From the last read: the power that would place a step since the last change is
		// not restored, and up to the last read the counter stood still. A daily counter
		// keeps its last change: it limits where a reset is placed (spec 5.3).
		r.life[cp.ref] = NewLifetime(cp.cfg, st.base, maxTime(st.at, st.seen))
		return
	}
	gr, ok := r.groups[cp.group]
	if !ok {
		gr = &groupRun{g: NewGroup(nil, rollover, loc, time.Time{}, time.Time{})}
		r.groups[cp.group] = gr
	}
	gr.g.members = append(gr.g.members, NewDaily(cp.cfg, st.base, st.at))
	gr.refs = append(gr.refs, cp.ref)
	gr.g.last = maxTime(gr.g.last, maxTime(st.at, st.seen))
	gr.g.lastReset = maxTime(gr.g.lastReset, st.resetAt)
}

// groupOf returns the reset group of a daily_reset counter (nil otherwise).
func (r *deviceRun) groupOf(x ref) *Group {
	for _, gr := range r.groups {
		for _, gx := range gr.refs {
			if gx == x {
				return gr.g
			}
		}
	}
	return nil
}

// isGap reports whether a poll at at does not follow the previous poll directly: a poll
// failed in between or the device was not read for a long time.
func (r *deviceRun) isGap(at time.Time) bool {
	return r.failed || r.isOutage(at)
}

// isOutage reports whether the device was not read for longer than a slow cycle (spec 5.2,
// 5.3). One failed poll in between is a slow cycle.
func (r *deviceRun) isOutage(at time.Time) bool {
	return r.lastOK.IsZero() || at.Before(r.lastOK) ||
		at.Sub(r.lastOK) > r.dev.Interval+GapAfter
}

// noteOutage remembers where an outage began for every counter: the delta that later moves
// its base covers the outage, also when that happens in a later run.
func (r *deviceRun) noteOutage(at time.Time) {
	if r.lastOK.IsZero() || !r.isOutage(at) {
		return
	}
	for _, cp := range r.plan.counters {
		if r.gapFrom[cp.ref].IsZero() {
			r.gapFrom[cp.ref] = r.lastOK
		}
	}
}

// addPower feeds the power of this poll into the shapes; a poll without a valid power is
// bridged by the next one.
func (r *deviceRun) addPower(vals decoded, at time.Time) {
	for _, cp := range r.plan.counters {
		s, ok := r.shapes[cp.ref]
		if p, valid := vals.get(cp.power); ok && valid {
			s.Add(p.Num, at)
		}
	}
}

// rebase starts a new span for counter x after its base moved at at.
func (r *deviceRun) rebase(x ref, at time.Time) {
	delete(r.gapFrom, x)
	if s, ok := r.shapes[x]; ok {
		s.Cut(at)
	}
}

// counterBase returns the base value and time of a counter before this poll.
func (r *deviceRun) counterBase(x ref) (*float64, time.Time) {
	if l, ok := r.life[x]; ok {
		return l.Base()
	}
	for _, gr := range r.groups {
		for i, gx := range gr.refs {
			if gx == x {
				return gr.g.members[i].Base()
			}
		}
	}
	return nil, time.Time{}
}

// deltas runs every counter tracker for one poll.
func (r *deviceRun) deltas(vals decoded, at time.Time, gap bool) map[ref]Delta {
	out := map[ref]Delta{}
	for x, l := range r.life {
		if v, ok := vals.get(x); ok {
			out[x] = l.Update(v.Num, at, gap)
		}
	}
	for _, gr := range r.groups {
		readings := make([]Reading, len(gr.refs))
		for i, x := range gr.refs {
			v, ok := vals.get(x)
			readings[i] = Reading{Value: v.Num, Valid: ok}
		}
		for i, d := range gr.g.Update(at, gap, readings) {
			out[gr.refs[i]] = d
		}
	}
	return out
}

// decoded holds the channel values of every block of a poll.
type decoded map[string]map[string]profile.Value

func (d decoded) get(x ref) (profile.Value, bool) {
	v, ok := d[x.block][x.channel]
	return v, ok && v.Valid
}

// decodeAll decodes the needed blocks of one poll; others were not read.
func decodeAll(p *profile.Profile, n profile.Needs, raw profile.Raw) decoded {
	out := decoded{}
	for _, b := range n.Blocks() {
		out[b] = p.Channels(b, raw)
	}
	return out
}

// flagsOf maps a counter delta to bucket flags. The estimated flag of an outage is set by
// markEstimated on the slots of the outage only.
func flagsOf(d Delta) int {
	f := 0
	if d.ResetSeen {
		f |= storage.FlagResetSeen
	}
	return f
}
