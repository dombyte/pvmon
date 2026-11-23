package service

import (
	"context"
	"fmt"
	"time"

	"github.com/dombyte/pvmon/internal/derive"
	"github.com/dombyte/pvmon/internal/report"
)

// addSlots sets the time-resolved estimates of days (one per day of rng) from the
// buckets, in chunks below the bucket limit.
func (r *Reports) addSlots(ctx context.Context, rng report.Range, days []report.Day) error {
	names := keysOf(r.plan.slot)
	lps := make([]string, len(r.plan.layout.Loadpoints))
	for i, m := range r.plan.layout.Loadpoints {
		lps[i] = m.ID
	}
	for start := 0; start < len(days); start += slotChunkDays {
		end := min(start+slotChunkDays, len(days)) - 1
		res, err := r.energy.Energy(ctx, names, derive.LevelBucket,
			r.local(rng.From.AddDate(0, 0, start)), r.local(rng.From.AddDate(0, 0, end)))
		if err != nil {
			return fmt.Errorf("service: report slots: %w", err)
		}
		byDay, err := r.slotsByDay(res)
		if err != nil {
			return err
		}
		for i := start; i <= end; i++ {
			days[i].Slots = report.EstimateDay(byDay[days[i].Date.Format(time.DateOnly)], lps)
		}
	}
	return nil
}

// slotsByDay turns bucket results into slots grouped by local date.
func (r *Reports) slotsByDay(res []derive.Result) (map[string][]report.Slot, error) {
	out := map[string][]report.Slot{}
	if len(res) == 0 {
		return out, nil
	}
	for j, pv := range res[0].Values {
		at, err := time.Parse(time.RFC3339, pv.Period)
		if err != nil {
			return nil, fmt.Errorf("service: report slot %q: %w", pv.Period, err)
		}
		s := report.Slot{At: at, Values: map[string]float64{}}
		for _, series := range res {
			v := series.Values[j]
			if !v.Complete || v.Value.Value == nil {
				s.Gap = true
				break
			}
			s.Values[r.plan.slot[series.Name]] = *v.Value.Value
		}
		date := at.Format(time.DateOnly)
		out[date] = append(out[date], s)
	}
	return out, nil
}
