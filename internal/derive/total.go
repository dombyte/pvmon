package derive

import (
	"context"
	"fmt"
	"time"

	"github.com/dombyte/pvmon/internal/storage"
)

// total is the lifetime of each series: carry + every year since the first stored slot.
func (s *Service) total(ctx context.Context, ids map[string]int64) (map[string]Agg, error) {
	first, err := s.firstSlot(ctx)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	start := time.Unix(min(first, Slot(now))*slotSeconds, 0)
	years, err := Periods(LevelYear, start, now, s.loc)
	if err != nil {
		return nil, err
	}
	out, err := s.sumParts(ctx, ids, LevelYear, years)
	if err != nil {
		return nil, err
	}
	return s.addCarry(ctx, ids, out)
}

func (s *Service) addCarry(ctx context.Context, ids map[string]int64, aggs map[string]Agg,
) (map[string]Agg, error) {
	list := make([]int64, 0, len(ids))
	for _, id := range ids {
		list = append(list, id)
	}
	carry, err := s.read.Carry(ctx, list)
	if err != nil {
		return nil, fmt.Errorf("derive: %w", err)
	}
	for name, id := range ids {
		if wh, ok := carry[id]; ok {
			aggs[name] = aggs[name].Plus(Agg{Wh: wh, Data: true})
		}
	}
	return aggs, nil
}

// bucketAggs reads the buckets of every series over all slot periods (one query per
// series) and returns one aggregate map per slot.
func (s *Service) bucketAggs(ctx context.Context, ids map[string]int64, periods []Period,
) ([]map[string]Agg, error) {
	out := make([]map[string]Agg, len(periods))
	for i := range out {
		out[i] = map[string]Agg{}
	}
	if len(periods) == 0 {
		return out, nil
	}
	from := periods[0].From
	for name, id := range ids {
		bs, err := s.read.EnergyBuckets(ctx, id, from, periods[len(periods)-1].To)
		if err != nil {
			return nil, fmt.Errorf("derive: %w", err)
		}
		for _, b := range bs {
			est := b.Flags&(storage.FlagEstimated|storage.FlagGap) != 0
			a := Agg{Wh: b.Wh, Data: true, Estimated: est}
			if b.Flags&storage.FlagGap != 0 {
				a.Missing = []string{periods[b.Slot-from].Key}
			}
			out[b.Slot-from][name] = a
		}
	}
	return out, nil
}
