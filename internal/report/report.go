package report

import (
	"sort"
	"time"
)

// maxListedDays caps the missing days listed by date; the count stays exact.
const maxListedDays = 100

// Report is the computed report.
type Report struct {
	Range       Range
	Granularity Granularity
	Layout      Layout
	// Summary covers the days with every core series.
	Summary  Summary
	Patterns Patterns
	// Buckets group the range by Granularity, oldest first, empty ones included.
	Buckets []Bucket
	// Sections; nil when the site has nothing to show (spec 12 table).
	PVSources  *Section
	Batteries  *Section
	Loadpoints *Loadpoints
	Profile    Profile
	// Comparison is nil without a comparison period.
	Comparison *Comparison
	Figures    []Figure
	// MissingDays are the days without every core series (at most maxListedDays).
	MissingDays []time.Time
	Quality     []MeterQuality
	// TodayExcluded is true when the requested range ended today and the report ends
	// yesterday.
	TodayExcluded bool
}

// Comparison is the comparison period.
type Comparison struct {
	Mode    CompareMode
	Range   Range
	Summary Summary
	// Matched is true when the periods cannot be compared as wholes (missing days or a
	// different length): the figures then compare only the days both periods have.
	Matched     bool
	MatchedDays int
}

// sides are the two summaries the changes of a comparison are computed from.
type sides struct {
	cur, base Summary
}

// Build computes the report.
func Build(in Input) Report {
	l := in.Layout
	all := within(in.Days, in.Range)
	days := filterDays(all, l.coreKeys())
	r := Report{
		Range: in.Range, Granularity: in.Granularity, Layout: l,
		Summary:   summarize(days, in.Range.Days()),
		Patterns:  patterns(days, l),
		Buckets:   buckets(in.Range, in.Granularity, days),
		PVSources: pvSources(days, l.PV), Batteries: batteries(days, l.Batteries),
		Loadpoints: loadpoints(days, l.Loadpoints), Profile: loadProfile(days),
		MissingDays: missing(in.Range, days), Quality: in.Quality,
		TodayExcluded: in.TodayExcluded,
	}
	var cmp *sides
	if in.Compare != nil {
		r.Comparison, cmp = compare(in.Compare, l, in.Range, days, r.Summary)
	}
	r.Figures = figures(l, r.Summary, cmp)
	return r
}

// missing lists the days of r not in days (sorted), at most maxListedDays.
func missing(r Range, days []Day) []time.Time {
	have := make(map[time.Time]bool, len(days))
	for _, d := range days {
		have[d.Date] = true
	}
	var out []time.Time
	for d := r.From; !d.After(r.To) && len(out) < maxListedDays; d = d.AddDate(0, 0, 1) {
		if !have[d] {
			out = append(out, d)
		}
	}
	return out
}

// compare evaluates the comparison period: both whole periods when they are complete
// and equally long, otherwise only their matched days.
func compare(c *CompareInput, l Layout, r Range, days []Day, cur Summary,
) (*Comparison, *sides) {
	baseDays := filterDays(within(c.Days, c.Range), l.coreKeys())
	base := summarize(baseDays, c.Range.Days())
	out := &Comparison{Mode: c.Mode, Range: c.Range, Summary: base}
	if cur.Complete() && base.Complete() && cur.Days == base.Days {
		return out, &sides{cur, base}
	}
	a, b := matchDays(days, baseDays, func(day time.Time) (time.Time, bool) {
		return partner(day, c.Mode, r, c.Range)
	})
	out.Matched, out.MatchedDays = true, len(a)
	return out, &sides{summarize(a, len(a)), summarize(b, len(b))}
}

// matchDays returns the days of cur whose partner day is in base, and those partners,
// pairwise in the same order.
func matchDays(cur, base []Day, partner func(time.Time) (time.Time, bool)) (a, b []Day) {
	byDate := make(map[time.Time]Day, len(base))
	for _, d := range base {
		byDate[d.Date] = d
	}
	for _, d := range cur {
		p, ok := partner(d.Date)
		if !ok {
			continue
		}
		if o, found := byDate[p]; found {
			a, b = append(a, d), append(b, o)
		}
	}
	return a, b
}

// partner returns the day of the comparison range cmp that day (of r) is compared
// with: the same date a year earlier for CompareLastYear (29 February has none),
// otherwise the day at the same position in cmp (none past its end, e.g. the 31st of a
// month compared with a 30-day month).
func partner(day time.Time, m CompareMode, r, cmp Range) (time.Time, bool) {
	if m == CompareLastYear {
		p := yearBefore(day)
		return p, p.Day() == day.Day()
	}
	p := cmp.From.AddDate(0, 0, Range{From: r.From, To: day}.Days()-1)
	return p, !p.After(cmp.To)
}

// within returns the days of r, oldest first.
func within(days []Day, r Range) []Day {
	out := make([]Day, 0, len(days))
	for _, d := range days {
		if r.Contains(d.Date) {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}
