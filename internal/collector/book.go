package collector

import (
	"fmt"
	"math"
	"time"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/storage"
)

// booking carries one poll's context to the booking steps.
type booking struct {
	acc    *Accumulator
	at     time.Time
	gap    bool
	vals   decoded
	deltas map[ref]Delta
	from   map[ref]time.Time
	res    *pollResult
	// gapFrom is the start of an outage that the delta of a counter covers, as before the
	// poll moved any base.
	gapFrom map[ref]time.Time
}

// bookCounters books direct counters and saves every counter state.
func (r *deviceRun) bookCounters(b *booking) {
	for _, cp := range r.plan.counters {
		d, read := b.deltas[cp.ref]
		if d.Event != "" {
			b.res.logs = append(b.res.logs,
				fmt.Sprintf("%s %s: counter %s", r.dev.ID, cp.ref, d.Event))
		}
		base, baseAt := r.counterBase(cp.ref)
		if base == nil {
			continue
		}
		if cp.book != nil && bookable(d, read) {
			r.bookDelta(b, cp, d, base)
		}
		if baseAt.Equal(b.at) {
			r.rebase(cp.ref, b.at)
		}
		b.acc.SetCounter(r.counterState(cp, base, baseAt, b.at))
	}
}

// bookDelta books a counter delta, shaped by the meter's power where it covers the span.
func (r *deviceRun) bookDelta(b *booking, cp counterPlan, d Delta, base *float64) {
	from := b.from[cp.ref]
	if s, ok := r.shapes[cp.ref]; !ok || !s.covers(from) {
		from = r.spreadFrom(b, cp.ref, d)
	}
	e := Energy{Wh: d.Wh, From: from, To: b.at, Flags: flagsOf(d), Counter: base}
	if d.Wh == 0 {
		// The slots up to the last booking have their rows; write only what changed.
		e.From = maxTime(e.From, r.booked[cp.ref])
	}
	var w map[int64]float64
	if s, ok := r.shapes[cp.ref]; ok {
		w = s.Weights(e.From, e.Wh)
	}
	b.acc.AddShaped(r.ids[*cp.book], e, w)
	r.booked[cp.ref] = b.at
	r.markEstimated(b, cp.ref, e.From, *cp.book)
}

// spreadFrom is where an even spread of delta d starts. Without power, nothing tells when
// the energy of an outage or an earlier run flowed since the base, but the counter stood
// still while it was read. A reset is placed by its group instead.
func (r *deviceRun) spreadFrom(b *booking, x ref, d Delta) time.Time {
	from := b.from[x]
	if d.ResetSeen {
		return from
	}
	if !b.gapFrom[x].IsZero() {
		from = maxTime(from, b.gapFrom[x])
	}
	return maxTime(from, r.read[x])
}

// markEstimated flags the slots from the outage start to this poll estimated: there the
// spread of a delta booked from from is a guess (spec 5.3). A delta in one slot is exact.
func (r *deviceRun) markEstimated(b *booking, x ref, from time.Time,
	series ...storage.SeriesKey,
) {
	g := b.gapFrom[x]
	if g.IsZero() || Slot(from) == Slot(b.at) {
		return
	}
	start := maxTime(g, from)
	if !b.at.After(start) {
		return
	}
	for _, s := range series {
		b.acc.AddEnergy(r.ids[s], Energy{From: start, To: b.at, Flags: storage.FlagEstimated})
	}
}

// bookable reports whether a counter delta is booked. 0 Wh is booked too: a read counter
// makes its slots complete (spec 5.4). A held reading books nothing: the counter buffers
// the energy and the poll that releases it books the whole range, so until then the slots
// stay open instead of showing a measured 0.
func bookable(d Delta, read bool) bool { return read && !d.Held && d.Wh >= 0 }

// counterState is the persisted state of a counter input after this poll.
func (r *deviceRun) counterState(cp counterPlan, base *float64, at, seen time.Time,
) storage.CounterState {
	st := storage.CounterState{
		SeriesID: r.ids[cp.input], Raw: base, At: at.UnixMilli(), Seen: seen.UnixMilli(),
		Resolution: cp.cfg.EffectiveResolution(*base),
	}
	if g := r.groupOf(cp.ref); g != nil && !g.LastReset().IsZero() {
		st.ResetAt = g.LastReset().UnixMilli()
	}
	if g := r.gapFrom[cp.ref]; !g.IsZero() {
		st.GapFrom = g.UnixMilli()
	}
	return st
}

// bookShares splits counter deltas over share channels by their (positive) power.
func (r *deviceRun) bookShares(b *booking) {
	byOf := map[ref][]sharePlan{}
	for _, sp := range r.plan.shares {
		byOf[sp.of] = append(byOf[sp.of], sp)
	}
	for of, shares := range byOf {
		d, read := b.deltas[of]
		if !bookable(d, read) {
			continue
		}
		weights, total := shareWeights(b.vals, shares)
		for i, sp := range shares {
			w := 1.0 / float64(len(sp.peers)) // no power anywhere: split evenly
			if total > 0 {
				w = weights[i] / total
			}
			b.acc.AddEnergy(r.ids[sp.series], Energy{
				Wh: d.Wh * w, From: r.spreadFrom(b, of, d), To: b.at,
				Flags: flagsOf(d) | storage.FlagEstimated,
			})
		}
	}
}

// shareWeights returns the positive power of each share and the total over all its peers,
// so a configured string gets its own part even when the other strings have no meter.
func shareWeights(vals decoded, shares []sharePlan) ([]float64, float64) {
	pos := func(r ref) float64 {
		if v, ok := vals.get(r); ok && v.Num > 0 {
			return v.Num
		}
		return 0
	}
	weights := make([]float64, len(shares))
	for i, sp := range shares {
		weights[i] = pos(sp.by)
	}
	total := 0.0
	if len(shares) > 0 {
		for _, peer := range shares[0].peers {
			total += pos(peer)
		}
	}
	return weights, total
}

// bookIntegrations integrates power of meters without energy channels (marked estimated).
func (r *deviceRun) bookIntegrations(b *booking) {
	for _, ip := range r.plan.integrates {
		v, ok := b.vals.get(ip.power)
		if !ok {
			r.integ[ip.power].Reset()
			r.markLost(b, ip)
			continue
		}
		wh, ok := r.integ[ip.power].Update(v.Num, b.at, b.gap)
		if !ok {
			r.markLost(b, ip)
			continue
		}
		key, other := ip.pos, ip.neg
		if wh < 0 {
			key, other, wh = ip.neg, ip.pos, -wh
		}
		for _, k := range []struct {
			series *storage.SeriesKey
			wh     float64
		}{{key, wh}, {other, 0}} {
			if k.series != nil {
				b.acc.AddEnergy(r.ids[*k.series], Energy{
					Wh: k.wh, From: r.lastOK, To: b.at, Flags: storage.FlagEstimated,
				})
			}
		}
	}
}

// markLost flags the span since the last successful poll as a gap: a power-only meter
// cannot tell its energy there, so those slots are missing, never complete (spec 5.3).
// Before the first poll of a run there is no span to flag.
func (r *deviceRun) markLost(b *booking, ip integratePlan) {
	if r.lastOK.IsZero() {
		return
	}
	for _, s := range []*storage.SeriesKey{ip.pos, ip.neg} {
		if s != nil {
			b.acc.AddEnergy(r.ids[*s], Energy{From: r.lastOK, To: b.at, Flags: storage.FlagGap})
		}
	}
}

// Netting methods (spec 6).
const (
	nettingCounters = "counters"
	nettingPower    = "power"
	// nettingDeadband is the smallest net that method 1 books. Its counters step by up to
	// 1 Wh, not in the same polls, so a difference below two steps can be their rounding.
	nettingDeadband = 2.0 // Wh
)

// bookNetting nets the grid meter per poll and books import or export.
func (r *deviceRun) bookNetting(b *booking) {
	np := r.plan.netting
	if np == nil {
		return
	}
	if !nettable(b, np) {
		return // nothing to net without both counters (first reading or invalid values)
	}
	method := r.nettingMethod(b, np)
	net, ok := r.netAmount(b, np, method)
	if method == nettingCounters && !b.gap {
		r.bookCancelled(b, np, min(b.deltas[np.imp].Wh, b.deltas[np.exp].Wh))
	}
	if ok {
		from := r.bookNet(b, np, net)
		r.markEstimated(b, np.imp, from, np.in, np.out)
	}
}

// netAmount is the net to book at this poll: import − export from the counters (method 1,
// and every gap poll) or from the integrated signed power (method 2), plus what method 1
// held back before.
func (r *deviceRun) netAmount(b *booking, np *nettingPlan, method string) (float64, bool) {
	if method == nettingPower && !b.gap {
		net, ok := r.netPower(b, np)
		if !ok {
			return 0, false
		}
		return r.takeNet(net), true
	}
	r.netInt.Reset()
	net := r.takeNet(b.deltas[np.imp].Wh - b.deltas[np.exp].Wh)
	if method == nettingCounters && !b.gap && math.Abs(net) < nettingDeadband {
		// shortcut: held in memory only, so a restart loses less than the deadband;
		// persist it in counter_state if that ever matters.
		r.netHeld, net = net, 0
	}
	return net, true
}

// takeNet adds the held net to net.
func (r *deviceRun) takeNet(net float64) float64 {
	net += r.netHeld
	r.netHeld = 0
	return net
}

// nettable reports whether both counters were read and a base time exists.
func nettable(b *booking, np *nettingPlan) bool {
	_, readImp := b.deltas[np.imp]
	_, readExp := b.deltas[np.exp]
	return readImp && readExp && !b.from[np.imp].IsZero()
}

// bookCancelled stores import and export that cancelled out within one poll (method 1).
func (r *deviceRun) bookCancelled(b *booking, np *nettingPlan, wh float64) {
	if wh >= 0 {
		// Cancelling happens within this poll, not since the last change of the counter.
		from := maxTime(b.from[np.imp], r.lastOK)
		b.acc.AddEnergy(r.ids[np.cancelled], Energy{Wh: wh, From: from, To: b.at})
	}
}

// netPower integrates the signed grid power (method 2).
func (r *deviceRun) netPower(b *booking, np *nettingPlan) (float64, bool) {
	p, valid := b.vals.get(np.power)
	if !valid {
		r.netInt.Reset()
		return 0, false
	}
	return r.netInt.Update(p.Num, b.at, false)
}

// bookNet books the net of this poll since the last successful poll and returns that
// start. Both counters were read there, so their deltas since their bases are the deltas
// since that poll, also when a counter has not moved for hours.
func (r *deviceRun) bookNet(b *booking, np *nettingPlan, net float64) time.Time {
	from := maxTime(b.from[np.imp], r.lastOK)
	key, other := np.in, np.out
	if net < 0 {
		key, other, net = np.out, np.in, -net
	}
	// The other direction gets 0 Wh, so both series have complete slots.
	b.acc.AddEnergy(r.ids[key], Energy{Wh: net, From: from, To: b.at})
	b.acc.AddEnergy(r.ids[other], Energy{From: from, To: b.at})
	return from
}

// nettingMethod picks method 1 while both counters resolve ≤ 1 Wh, else method 2, and
// logs a switch once.
func (r *deviceRun) nettingMethod(b *booking, np *nettingPlan) string {
	method := nettingCounters
	for _, x := range []ref{np.imp, np.exp} {
		base, _ := r.counterBase(x)
		if base == nil || r.counterCfg(x).EffectiveResolution(*base) > 1 {
			method = nettingPower
		}
	}
	if method != r.netMode {
		b.res.logs = append(b.res.logs, fmt.Sprintf("%s: netting uses %s", r.dev.ID, method))
		r.netMode = method
	}
	return method
}

func (r *deviceRun) counterCfg(x ref) CounterConfig {
	for _, cp := range r.plan.counters {
		if cp.ref == x {
			return cp.cfg
		}
	}
	return CounterConfig{}
}

// bookGauges adds every historised gauge channel.
func (r *deviceRun) bookGauges(b *booking) {
	for _, gp := range r.plan.gauges {
		if v, ok := b.vals.get(gp.ref); ok {
			b.acc.AddGauge(r.ids[gp.series], v.Num, b.at, r.dev.Interval)
		}
	}
}

// statusChanges compares the status with the last one and returns the changes.
func (r *deviceRun) statusChanges(raw profile.Raw, at time.Time) []storage.StatusEvent {
	var out []storage.StatusEvent
	for name, sv := range r.dev.Profile.Statuses(raw) {
		code := int64(sv.Code)
		if last, seen := r.status[name]; !sv.Valid || (seen && last == code) {
			continue
		}
		r.status[name] = code
		out = append(out, storage.StatusEvent{
			Device: r.dev.ID, Register: name, At: at.UnixMilli(), Code: code,
		})
	}
	return out
}

// seedStatus restores the last stored codes, so a restart stores no unchanged state.
func (r *deviceRun) seedStatus(last map[string]storage.StatusEvent) {
	for name, e := range last {
		if name == storage.ConnectionRegister {
			r.conn = e.Code
			continue
		}
		r.status[name] = e.Code
	}
}

// connectionChange is the event of a new connection code, or false when it is stored.
func (r *deviceRun) connectionChange(code int64, at time.Time) (storage.StatusEvent, bool) {
	if r.conn == code {
		return storage.StatusEvent{}, false
	}
	r.conn = code
	return storage.StatusEvent{
		Device: r.dev.ID, Register: storage.ConnectionRegister, At: at.UnixMilli(), Code: code,
	}, true
}
