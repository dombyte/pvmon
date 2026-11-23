package maintenance

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
)

// Import tolerances: a carry below half a watt-hour is rounding; a period whose stored
// solis value differs from its daily sum by more than 0.05 kWh is reported.
const (
	whPerKWh        = 1000
	carryEpsilonWh  = 0.5
	periodTolerance = 0.05
)

// ImportJob bundles the dependencies of import-solis (spec 7.8).
type ImportJob struct {
	Source   SolisSource
	Target   storage.ImportStore
	Map      *SolisMap
	Series   map[string]storage.SeriesDef // from SolisMap.Resolve
	Location *time.Location
	Clock    util.Clock
	Name     string // recorded as import source (file name of the solis database)
}

// SeriesReport is the import result of one solis energy series.
type SeriesReport struct {
	Key      string // solis key without level suffix
	Target   string // <meter>/<channel>
	Days     int    // imported days
	KWh      float64
	Later    int // days on or after the cutoff, left to pvmon's own data
	Negative int // negative daily values, not imported
	CarryKWh float64
	// TotalBelow is set when the solis total is lower than its daily sum (no carry).
	TotalBelow bool
}

// PeriodMismatch is a solis month or year whose stored value is not its daily sum
// (cutover offsets, purged days); pvmon only has the daily values (spec 7.8).
type PeriodMismatch struct {
	Key, Level, Period string
	Stored, Daily      float64
	Days               int // solis days of the period
}

// ImportReport is the summary printed by import-solis.
type ImportReport struct {
	Cutoff     string // first day not imported
	Series     []SeriesReport
	Unmapped   []string // solis keys with daily values that the map does not name
	Status     int      // imported status changes
	StatusKeys []string // solis status keys that the map does not name
	Mismatches []PeriodMismatch
}

// ImportSolis imports a solis database (spec 7.8): each daily value becomes one bucket in
// the last slot of its local day (estimated|imported), the difference between a solis
// total and its daily sum becomes carry, and the status log becomes status_event. Days
// from today or from the first day pvmon already stored on are skipped.
func ImportSolis(ctx context.Context, job ImportJob) (*ImportReport, error) {
	if src, err := job.Target.ImportSource(ctx); err != nil {
		return nil, err
	} else if src != "" {
		return nil, fmt.Errorf("%w (%s)", storage.ErrAlreadyImported, src)
	}
	cutoff, err := job.cutoff(ctx)
	if err != nil {
		return nil, err
	}
	rep := &ImportReport{Cutoff: cutoff.Format(time.DateOnly)}
	batch := storage.ImportBatch{Source: job.Name}
	if batch.Series, err = job.energy(ctx, cutoff, rep); err != nil {
		return nil, err
	}
	if batch.Status, err = job.status(ctx, cutoff, rep); err != nil {
		return nil, err
	}
	if rep.Mismatches, err = job.mismatches(ctx); err != nil {
		return nil, err
	}
	return rep, job.Target.Import(ctx, batch)
}

// cutoff is the earlier of today's and the first stored day's local midnight.
func (j ImportJob) cutoff(ctx context.Context) (time.Time, error) {
	cut := util.Midnight(j.Clock.Now(), j.Location)
	slot, ok, err := j.Target.FirstSlot(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if first := util.Midnight(time.Unix(slot*storage.SlotSeconds, 0), j.Location); ok &&
		first.Before(cut) {
		cut = first
	}
	return cut, nil
}

// energy builds the imported series and fills their reports.
func (j ImportJob) energy(ctx context.Context, cutoff time.Time, rep *ImportReport,
) ([]storage.ImportSeries, error) {
	daily, err := j.Source.Values(ctx, LevelDaily)
	if err != nil {
		return nil, err
	}
	totals, err := j.Source.Values(ctx, LevelTotal)
	if err != nil {
		return nil, err
	}
	byKey := groupByBase(daily)
	rep.Unmapped = unmapped(byKey, j.Series)
	var out []storage.ImportSeries
	for _, key := range sortedKeys(j.Series) {
		s, r, err := j.series(key, byKey[key], cutoff)
		if err != nil {
			return nil, err
		}
		total, ok := totalOf(totals, key)
		s.Carry = carry(&r, byKey[key], total, ok)
		out = append(out, s)
		rep.Series = append(rep.Series, r)
	}
	return out, nil
}

func groupByBase(vs []SolisValue) map[string][]SolisValue {
	out := map[string][]SolisValue{}
	for _, v := range vs {
		out[v.Base] = append(out[v.Base], v)
	}
	return out
}

func unmapped[V any](byKey map[string][]SolisValue, mapped map[string]V) []string {
	var out []string
	for key := range byKey {
		if _, ok := mapped[key]; !ok {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// series converts the daily values of one key into buckets before cutoff.
func (j ImportJob) series(key string, days []SolisValue, cutoff time.Time,
) (storage.ImportSeries, SeriesReport, error) {
	def := j.Series[key]
	s := storage.ImportSeries{Def: def}
	r := SeriesReport{Key: key, Target: def.Meter + "/" + def.Channel}
	for _, v := range days {
		start, err := time.ParseInLocation(time.DateOnly, v.Period, j.Location)
		if err != nil {
			return s, r, fmt.Errorf("maintenance: solis %s day %q: %w", key, v.Period, err)
		}
		switch {
		case !start.Before(cutoff):
			r.Later++
		case v.KWh < 0:
			r.Negative++
		default:
			s.Buckets = append(s.Buckets, storage.EnergyBucket{
				Slot: lastSlot(start), Wh: v.KWh * whPerKWh,
				Flags: storage.FlagEstimated | storage.FlagImported,
			})
			r.Days++
			r.KWh += v.KWh
		}
	}
	return s, r, nil
}

// lastSlot is the last 15-minute slot of the local day starting at start.
func lastSlot(start time.Time) int64 {
	next := start.AddDate(0, 0, 1)
	return next.Unix()/storage.SlotSeconds - 1
}

func totalOf(totals []SolisValue, key string) (float64, bool) {
	for _, t := range totals {
		if t.Base == key {
			return t.KWh, true
		}
	}
	return 0, false
}

// carry is the part of the solis total not covered by its daily values (baselines of
// purged days); a total below the daily sum is reported, never carried negative.
func carry(r *SeriesReport, days []SolisValue, total float64, ok bool) float64 {
	if !ok {
		return 0
	}
	sum := 0.0
	for _, v := range days {
		sum += v.KWh
	}
	diff := (total - sum) * whPerKWh
	switch {
	case diff > carryEpsilonWh:
		r.CarryKWh = diff / whPerKWh
		return diff
	case diff < -carryEpsilonWh:
		r.TotalBelow = true
	}
	return 0
}

// status converts the mapped status log before cutoff.
func (j ImportJob) status(ctx context.Context, cutoff time.Time, rep *ImportReport,
) ([]storage.StatusEvent, error) {
	log, err := j.Source.Status(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []storage.StatusEvent
	for _, st := range log {
		reg, ok := j.Map.Status.Registers[st.Key]
		switch {
		case !ok:
			seen[st.Key] = true
		case st.At.Before(cutoff):
			out = append(out, storage.StatusEvent{
				Device: j.Map.Status.Device, Register: reg, At: st.At.UnixMilli(),
				Code: st.Code,
			})
		}
	}
	rep.Status = len(out)
	rep.StatusKeys = sortedKeys(seen)
	return out, nil
}

// mismatches lists the solis months and years of mapped keys whose stored value is not
// the sum of their daily values. A zero row without daily values is "no data", not 0.
func (j ImportJob) mismatches(ctx context.Context) ([]PeriodMismatch, error) {
	daily, err := j.Source.Values(ctx, LevelDaily)
	if err != nil {
		return nil, err
	}
	var out []PeriodMismatch
	for _, level := range []string{LevelMonthly, LevelYearly} {
		stored, err := j.Source.Values(ctx, level)
		if err != nil {
			return nil, err
		}
		out = append(out, j.compare(level, stored, daily)...)
	}
	return out, nil
}

func (j ImportJob) compare(level string, stored, daily []SolisValue) []PeriodMismatch {
	sums := periodSums(level, daily)
	var out []PeriodMismatch
	for _, v := range stored {
		if _, ok := j.Series[v.Base]; !ok {
			continue
		}
		a := sums[v.Base+"|"+v.Period]
		if math.Abs(v.KWh-a.kwh) <= periodTolerance || (v.KWh == 0 && a.days == 0) {
			continue
		}
		out = append(out, PeriodMismatch{
			Key: v.Base, Level: level, Period: v.Period, Stored: v.KWh, Daily: a.kwh,
			Days: a.days,
		})
	}
	return out
}

// periodSum is the daily sum of one solis key over one month or year.
type periodSum struct {
	kwh  float64
	days int
}

// periodSums sums the daily values per "<key>|<period>" of level.
func periodSums(level string, daily []SolisValue) map[string]periodSum {
	n := periodLen(level)
	sums := map[string]periodSum{}
	for _, d := range daily {
		if len(d.Period) < n {
			continue // not a date; series() already rejects it for mapped keys
		}
		k := d.Base + "|" + d.Period[:n]
		a := sums[k]
		sums[k] = periodSum{kwh: a.kwh + d.KWh, days: a.days + 1}
	}
	return sums
}

// periodLen is the length of a period string of level ("2006-01" or "2006").
func periodLen(level string) int {
	const month, year = len("2006-01"), len("2006")
	if level == LevelMonthly {
		return month
	}
	return year
}
