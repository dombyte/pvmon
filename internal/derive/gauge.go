package derive

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/dombyte/pvmon/internal/storage"
)

// GaugeResult is one requested gauge series over the requested days.
type GaugeResult struct {
	Name   string
	Unit   string
	Points []GaugePoint // slots with data only, oldest first
}

// GaugeNames returns every name Gauge accepts, sorted.
func (s *Service) GaugeNames() []string {
	out := make([]string, 0, len(s.gauges))
	for n := range s.gauges {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Gauge returns the 15-minute gauges of names for the local days from..to.
func (s *Service) Gauge(ctx context.Context, names []string, from, to time.Time,
) ([]GaugeResult, error) {
	formulas := make([]GaugeFormula, len(names))
	var unknown []string
	for i, n := range names {
		f, ok := s.gauges[n]
		if !ok {
			unknown = append(unknown, n)
		}
		formulas[i] = f
	}
	if len(unknown) > 0 {
		return nil, &UnknownSeriesError{Names: unknown}
	}
	periods, err := Periods(LevelBucket, from, to, s.loc)
	if err != nil {
		return nil, err
	}
	first, end := periods[0].From, periods[len(periods)-1].To
	buckets, err := s.gaugeBuckets(ctx, formulas, first, end)
	if err != nil {
		return nil, err
	}
	out := make([]GaugeResult, len(names))
	for i, n := range names {
		out[i] = GaugeResult{Name: n, Unit: formulas[i].Unit, Points: formulas[i].Combine(buckets)}
	}
	return out, nil
}

// gaugeBuckets reads every stored series the formulas use; never stored = no data.
func (s *Service) gaugeBuckets(ctx context.Context, fs []GaugeFormula, from, to int64,
) (map[string][]storage.GaugeBucket, error) {
	out := map[string][]storage.GaugeBucket{}
	for _, f := range fs {
		for _, t := range f.Terms {
			if _, done := out[t.Series]; done {
				continue
			}
			id, err := s.seriesID(ctx, t.Series)
			if err != nil {
				return nil, err
			}
			out[t.Series] = nil
			if id == 0 {
				continue
			}
			b, err := s.read.GaugeBuckets(ctx, id, from, to)
			if err != nil {
				return nil, fmt.Errorf("derive: gauge: %w", err)
			}
			out[t.Series] = b
		}
	}
	return out, nil
}
