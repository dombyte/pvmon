package derive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
)

// Reader is the part of the read store derive needs.
type Reader interface {
	SeriesID(ctx context.Context, key storage.SeriesKey) (int64, error)
	SumEnergyMany(ctx context.Context, ids []int64, from, to, closedBefore int64,
	) (map[int64]storage.EnergySum, error)
	EnergyBuckets(ctx context.Context, seriesID, from, to int64) ([]storage.EnergyBucket, error)
	GaugeBuckets(ctx context.Context, seriesID, from, to int64) ([]storage.GaugeBucket, error)
	Carry(ctx context.Context, ids []int64) (map[int64]float64, error)
	FirstSlot(ctx context.Context) (int64, bool, error)
}

// Cache is the derived cache (derive write domain).
type Cache interface {
	CacheGet(ctx context.Context, keys []string, configRev int64) (map[string]string, error)
	CacheGen() int64
	CachePut(ctx context.Context, configRev, gen int64, entries map[string]string) error
}

// UnknownSeriesError lists requested names that are not in the catalogue.
type UnknownSeriesError struct{ Names []string }

func (e *UnknownSeriesError) Error() string {
	return "derive: unknown series " + strings.Join(e.Names, ", ")
}

// Result is one requested series over the requested periods.
type Result struct {
	Name   string
	Ratio  bool // values are percent instead of Wh
	Values []PeriodValue
}

// PeriodValue is the value of one period.
type PeriodValue struct {
	Period string
	Value
}

// Service answers energy requests from buckets and the derived cache.
type Service struct {
	cat    map[string]Formula
	gauges map[string]GaugeFormula
	read   Reader
	cache  Cache
	clock  util.Clock
	loc    *time.Location
	rev    int64        // config_rev: cache entries of other revisions are stale
	first  atomic.Int64 // first stored slot + 1; 0 = not known yet
}

// New builds the service for site s.
func New(s *site.Site, read Reader, cache Cache, clock util.Clock, loc *time.Location,
) (*Service, error) {
	if err := util.RequireAll("derive", util.D("site", s), util.D("reader", read),
		util.D("cache", cache), util.D("clock", clock), util.D("location", loc)); err != nil {
		return nil, err
	}
	return &Service{
		cat: Catalogue(s), gauges: GaugeCatalogue(s), read: read, cache: cache, clock: clock,
		loc: loc, rev: s.Rev,
	}, nil
}

// Names returns every name Energy accepts, sorted.
func (s *Service) Names() []string {
	out := make([]string, 0, len(s.cat))
	for n := range s.cat {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Energy returns names per period of level for the local days from..to.
func (s *Service) Energy(ctx context.Context, names []string, level Level, from, to time.Time,
) ([]Result, error) {
	formulas, err := s.formulas(names)
	if err != nil {
		return nil, err
	}
	periods, err := Periods(level, from, to, s.loc)
	if err != nil {
		return nil, err
	}
	ids, err := s.resolve(ctx, formulas)
	if err != nil {
		return nil, err
	}
	perPeriod, err := s.periodAggs(ctx, ids, level, periods)
	if err != nil {
		return nil, err
	}
	out := make([]Result, len(names))
	for i, n := range names {
		out[i] = Result{Name: n, Ratio: formulas[i].Ratio()}
		for j, p := range periods {
			out[i].Values = append(out[i].Values,
				PeriodValue{Period: p.Key, Value: formulas[i].Evaluate(perPeriod[j])})
		}
	}
	return out, nil
}

func (s *Service) formulas(names []string) ([]Formula, error) {
	out := make([]Formula, len(names))
	var unknown []string
	for i, n := range names {
		f, ok := s.cat[n]
		if !ok {
			unknown = append(unknown, n)
		}
		out[i] = f
	}
	if len(unknown) > 0 {
		return nil, &UnknownSeriesError{Names: unknown}
	}
	return out, nil
}

// resolve maps every stored series the formulas use to its ID; series that were never
// stored are left out and count as "no data".
func (s *Service) resolve(ctx context.Context, fs []Formula) (map[string]int64, error) {
	names := map[string]bool{}
	for _, f := range fs {
		for _, t := range concat(f.Terms, f.Den) {
			names[t.Series] = true
		}
	}
	ids := map[string]int64{}
	for name := range names {
		id, err := s.seriesID(ctx, name)
		if err != nil {
			return nil, err
		}
		if id != 0 {
			ids[name] = id
		}
	}
	return ids, nil
}

// seriesID returns 0 for a series that was never stored.
func (s *Service) seriesID(ctx context.Context, name string) (int64, error) {
	meter, channel, _ := strings.Cut(name, "/")
	id, err := s.read.SeriesID(ctx, storage.SeriesKey{Meter: meter, Channel: channel})
	if errors.Is(err, storage.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("derive: %w", err)
	}
	return id, nil
}

func (s *Service) periodAggs(ctx context.Context, ids map[string]int64, level Level,
	periods []Period,
) ([]map[string]Agg, error) {
	if level == LevelBucket {
		return s.bucketAggs(ctx, ids, periods)
	}
	out := make([]map[string]Agg, len(periods))
	for i, p := range periods {
		var err error
		if level == LevelTotal {
			out[i], err = s.total(ctx, ids)
		} else {
			out[i], err = s.aggs(ctx, ids, level, p)
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// firstSlot returns the first stored slot (cached once data exists); math.MaxInt64-like
// "now" when nothing is stored, so no day counts as missing.
func (s *Service) firstSlot(ctx context.Context) (int64, error) {
	if v := s.first.Load(); v > 0 {
		return v - 1, nil
	}
	slot, ok, err := s.read.FirstSlot(ctx)
	if err != nil {
		return 0, fmt.Errorf("derive: %w", err)
	}
	if !ok {
		return Slot(s.clock.Now()) + 1, nil
	}
	s.first.Store(slot + 1)
	return slot, nil
}

// aggs returns the aggregates of a day, month or year; closed periods go through the cache.
// A period that ends before the first stored slot has no data and nothing missing; it is
// answered without SQL and never cached, so scrolling back through empty years leaves no
// cache rows behind.
func (s *Service) aggs(ctx context.Context, ids map[string]int64, level Level, p Period,
) (map[string]Agg, error) {
	if p.To > Slot(util.Midnight(s.clock.Now(), s.loc)) {
		return s.compute(ctx, ids, level, p)
	}
	first, err := s.firstSlot(ctx)
	if err != nil {
		return nil, err
	}
	if p.To <= first {
		return map[string]Agg{}, nil
	}
	return s.closedAggs(ctx, ids, level, p)
}

// closedAggs serves a closed period from the cache and computes and stores what is missing.
func (s *Service) closedAggs(ctx context.Context, ids map[string]int64, level Level, p Period,
) (map[string]Agg, error) {
	cached, err := s.cached(ctx, ids, level, p)
	if err != nil {
		return nil, err
	}
	todo := map[string]int64{}
	for name, id := range ids {
		if _, ok := cached[name]; !ok {
			todo[name] = id
		}
	}
	if len(todo) == 0 {
		return cached, nil
	}
	gen := s.cache.CacheGen() // before the buckets are read
	fresh, err := s.compute(ctx, todo, level, p)
	if err != nil {
		return nil, err
	}
	if err := s.store(ctx, level, p, gen, fresh); err != nil {
		return nil, err
	}
	maps.Copy(cached, fresh)
	return cached, nil
}

func (s *Service) cached(ctx context.Context, ids map[string]int64, level Level, p Period,
) (map[string]Agg, error) {
	keys := map[string]string{}
	list := make([]string, 0, len(ids))
	for name := range ids {
		k := cacheKey(name, level, p)
		keys[k] = name
		list = append(list, k)
	}
	raw, err := s.cache.CacheGet(ctx, list, s.rev)
	if err != nil {
		return nil, fmt.Errorf("derive: %w", err)
	}
	out := map[string]Agg{}
	for k, v := range raw {
		var a Agg
		if json.Unmarshal([]byte(v), &a) == nil {
			out[keys[k]] = a
		}
	}
	return out, nil
}

func (s *Service) store(ctx context.Context, level Level, p Period, gen int64,
	aggs map[string]Agg,
) error {
	entries := map[string]string{}
	for name, a := range aggs {
		b, err := json.Marshal(a)
		if err != nil {
			return fmt.Errorf("derive: encode %s: %w", name, err)
		}
		entries[cacheKey(name, level, p)] = string(b)
	}
	if err := s.cache.CachePut(ctx, s.rev, gen, entries); err != nil {
		return fmt.Errorf("derive: %w", err)
	}
	return nil
}

// compute builds a period from its parts: days from SQL, months from days, years from
// months.
func (s *Service) compute(ctx context.Context, ids map[string]int64, level Level, p Period,
) (map[string]Agg, error) {
	switch level {
	case LevelMonth:
		return s.sumParts(ctx, ids, LevelDay, Days(p, s.loc))
	case LevelYear:
		return s.sumParts(ctx, ids, LevelMonth, Months(p, s.loc))
	default:
		return s.day(ctx, ids, p)
	}
}

func (s *Service) sumParts(ctx context.Context, ids map[string]int64, level Level,
	parts []Period,
) (map[string]Agg, error) {
	out := map[string]Agg{}
	for _, part := range parts {
		aggs, err := s.aggs(ctx, ids, level, part)
		if err != nil {
			return nil, err
		}
		for name := range ids {
			out[name] = out[name].Plus(aggs[name])
		}
	}
	return out, nil
}

// day sums one local day in one grouped query.
func (s *Service) day(ctx context.Context, ids map[string]int64, p Period,
) (map[string]Agg, error) {
	first, err := s.firstSlot(ctx)
	if err != nil {
		return nil, err
	}
	now := Slot(s.clock.Now())
	expected := int(max(0, min(p.To, now)-max(p.From, first)))
	list := make([]int64, 0, len(ids))
	for _, id := range ids {
		list = append(list, id)
	}
	sums, err := s.read.SumEnergyMany(ctx, list, p.From, p.To, now)
	if err != nil {
		return nil, fmt.Errorf("derive: %w", err)
	}
	out := map[string]Agg{}
	for name, id := range ids {
		out[name] = dayAgg(p.Key, sums[id], expected)
	}
	return out, nil
}

// cacheKey is "<series>|<level>|<period>" (spec 7.1), the form storage invalidates.
func cacheKey(name string, level Level, p Period) string {
	return name + "|" + string(level) + "|" + p.Key
}
