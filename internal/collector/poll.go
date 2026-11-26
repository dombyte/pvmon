package collector

import (
	"context"
	"time"

	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/profile"
)

// pollBudgetMargin keeps a poll inside its interval (spec 4.3: interval − 100 ms).
const pollBudgetMargin = 100 * time.Millisecond

// poll fetches and books one device poll; it returns the error of a failed fetch.
func (c *Collector) poll(ctx context.Context, r *deviceRun) (pollResult, error) {
	budget := max(r.dev.Interval-pollBudgetMargin, pollBudgetMargin)
	pctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	start := c.d.Clock.Now()
	sample, err := r.src.Fetch(pctx)
	var raw profile.Raw
	if err == nil {
		raw, err = sample.Raw()
	}
	// The poll time runs from the first request to the decoded sample (no booking).
	c.d.Reporter.DevicePoll(r.dev.ID, health.Poll{
		At: start, Took: c.d.Clock.Now().Sub(start), OK: err == nil,
	})
	if err != nil {
		r.failed = true
		return pollResult{}, err
	}
	return c.process(r, raw, sample.At), nil
}

// process books one successful poll.
func (c *Collector) process(r *deviceRun, raw profile.Raw, at time.Time) pollResult {
	res := pollResult{}
	vals := decodeAll(r.dev.Profile.Profile, r.plan.needs, raw)
	gap := r.isGap(at)
	from := map[ref]time.Time{}
	for _, cp := range r.plan.counters {
		_, baseAt := r.counterBase(cp.ref)
		// Without a base (first poll ever) nothing is known about the day so far.
		gr, ok := r.groups[cp.group]
		if ok && cp.kind == profile.CounterDailyReset && !baseAt.IsZero() {
			baseAt = maxTime(baseAt, gr.g.PeriodStart(at))
		}
		from[cp.ref] = baseAt
	}
	b := &booking{acc: c.acc, at: at, gap: gap, vals: vals, from: from, res: &res}
	b.deltas = r.deltas(vals, at, gap)
	for x, d := range b.deltas {
		if !d.From.IsZero() {
			b.from[x] = d.From // a confirmed reset happened after the poll before the drop
		}
	}
	r.bookCounters(b)
	r.bookShares(b)
	r.bookIntegrations(b)
	r.bookNetting(b)
	r.bookGauges(b)
	c.publishLive(r, vals, at)
	res.statusEvents = r.statusChanges(raw, at)
	r.lastOK, r.failed = at, false
	return res
}

func (c *Collector) publishLive(r *deviceRun, vals decoded, at time.Time) {
	for _, lp := range r.plan.live {
		blk := r.dev.Profile.Blocks[lp.block]
		for ch, v := range vals[lp.block] {
			if v.Valid {
				c.cache.Set(lp.meter+"/"+ch, livecache.Value{
					Num: v.Num, Text: v.Text,
					Unit: profile.NormalUnit(blk.Channels[ch].Unit), At: at,
				}, r.dev.Interval)
			}
		}
	}
}
