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
		status: map[string]int64{}, conn: -1, logged: map[string]bool{},
	}
	for _, cp := range pl.counters {
		st, ok := states[ids[cp.input]]
		rs := counterRestore{}
		if ok && st.Raw != nil {
			rs.base, rs.at, rs.seen = st.Raw, time.UnixMilli(st.At), time.UnixMilli(st.Seen)
			r.lastOK = maxTime(r.lastOK, rs.at)
		}
		if ok && st.ResetAt > 0 {
			rs.resetAt = time.UnixMilli(st.ResetAt)
		}
		r.addCounter(cp, rs, loc, rollover)
	}
	for _, ip := range pl.integrates {
		r.integ[ip.power] = &Integrator{}
	}
	return r
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
		r.life[cp.ref] = NewLifetime(cp.cfg, st.base, st.at)
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

// isGap reports whether a poll at at is a gap poll.
func (r *deviceRun) isGap(at time.Time) bool {
	return r.failed || r.lastOK.IsZero() || at.Before(r.lastOK) ||
		at.Sub(r.lastOK) > r.dev.Interval+GapAfter
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

func flagsOf(d Delta) int {
	f := 0
	if d.Gap {
		f |= storage.FlagEstimated | storage.FlagGap
	}
	if d.ResetSeen {
		f |= storage.FlagResetSeen
	}
	return f
}
