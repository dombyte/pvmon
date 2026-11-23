// Package derive turns 15-minute buckets into periods (day, month, year, total) and site
// quantities, caching closed periods in derived_cache (spec 5.1 step 8, 7.3). The period
// and aggregate math in period.go, agg.go and formula.go is pure: no I/O, clock or logging.
package derive

import (
	"errors"
	"time"

	"github.com/dombyte/pvmon/internal/util"
)

// Level is the granularity of a request.
type Level string

// Levels (spec 9.1 /api/v1/energy).
const (
	LevelBucket Level = "bucket"
	LevelDay    Level = "day"
	LevelMonth  Level = "month"
	LevelYear   Level = "year"
	LevelTotal  Level = "total"
)

// slotSeconds is the bucket length (spec 5.2).
const slotSeconds = 900

// Range limits (spec 9.1).
const (
	MaxDays    = 3660
	MaxBuckets = 10000
)

// Errors of a request; the HTTP layer maps them to error codes.
var (
	ErrInvalidLevel = errors.New("derive: invalid level")
	ErrInvalidRange = errors.New("derive: to must not be before from")
	ErrRangeTooLong = errors.New("derive: range too long")
)

// Period is a named slot range [From, To).
type Period struct {
	Key  string
	From int64
	To   int64
}

// Slot returns the bucket slot of t.
func Slot(t time.Time) int64 { return t.Unix() / slotSeconds }

// Periods returns the periods of level covering the local days from..to (inclusive).
// Months and years are whole even when from/to fall inside them. Bucket periods are
// the slots of [from, to+1 day).
func Periods(level Level, from, to time.Time, loc *time.Location) ([]Period, error) {
	start, end := util.Midnight(from, loc), util.Midnight(to, loc)
	if end.Before(start) {
		return nil, ErrInvalidRange
	}
	end = end.AddDate(0, 0, 1)
	switch level {
	case LevelDay:
		return days(start, end)
	case LevelMonth:
		return calendar(time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, loc), end,
			0, 1, "2006-01"), nil
	case LevelYear:
		return calendar(time.Date(start.Year(), 1, 1, 0, 0, 0, 0, loc), end,
			1, 0, "2006"), nil
	case LevelTotal:
		return []Period{{Key: "total", From: Slot(start), To: Slot(end)}}, nil
	case LevelBucket:
		return buckets(start, end, loc)
	}
	return nil, ErrInvalidLevel
}

func days(start, end time.Time) ([]Period, error) {
	if end.Sub(start) > MaxDays*24*time.Hour {
		return nil, ErrRangeTooLong
	}
	return calendar(start, end, 0, 0, time.DateOnly), nil
}

// calendar steps from start by years/months (or one day when both are 0) until end.
func calendar(start, end time.Time, years, months int, layout string) []Period {
	var out []Period
	for t := start; t.Before(end); {
		next := t.AddDate(years, months, 0)
		if years == 0 && months == 0 {
			next = t.AddDate(0, 0, 1)
		}
		out = append(out, Period{Key: t.Format(layout), From: Slot(t), To: Slot(next)})
		t = next
	}
	return out
}

func buckets(start, end time.Time, loc *time.Location) ([]Period, error) {
	from, to := Slot(start), Slot(end)
	if to-from > MaxBuckets {
		return nil, ErrRangeTooLong
	}
	out := make([]Period, 0, to-from)
	for s := from; s < to; s++ {
		key := time.Unix(s*slotSeconds, 0).In(loc).Format(time.RFC3339)
		out = append(out, Period{Key: key, From: s, To: s + 1})
	}
	return out, nil
}

// Days returns the day periods inside p (a day, month or year period).
func Days(p Period, loc *time.Location) []Period {
	start := time.Unix(p.From*slotSeconds, 0).In(loc)
	end := time.Unix(p.To*slotSeconds, 0).In(loc)
	return calendar(start, end, 0, 0, time.DateOnly)
}

// Months returns the month periods inside a year period.
func Months(p Period, loc *time.Location) []Period {
	start := time.Unix(p.From*slotSeconds, 0).In(loc)
	end := time.Unix(p.To*slotSeconds, 0).In(loc)
	return calendar(start, end, 0, 1, "2006-01")
}
