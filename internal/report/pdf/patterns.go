package pdf

import (
	"strings"
	"time"

	"github.com/dombyte/pvmon/internal/report"
)

func patternsSection(d *doc, r report.Report) {
	t := d.t
	p := r.Patterns
	d.heading(t.s("patterns"))
	var rows [][2]string
	add := func(key string, v *report.DayValue) {
		if v != nil {
			rows = append(rows, [2]string{t.s(key), t.kwhOf(v.Value) + " · " + t.weekday(v.Date)})
		}
	}
	add("best.pv", p.BestPV)
	add("lowest.pv", p.LowestPV)
	add("max.consumption", p.MaxConsumption)
	add("max.import", p.MaxImport)
	rows = append(rows,
		[2]string{t.s("weekday.consumption"), t.kwh(p.WeekdayConsumption)},
		[2]string{t.s("weekend.consumption"), t.kwh(p.WeekendConsumption)},
		[2]string{
			t.f("low.import", t.kwhOf(report.LowImportBelow)),
			t.plural(p.LowImportDays, "days"),
		})
	keyRows(d, rows)
}

func comparisonSection(d *doc, r report.Report) {
	t := d.t
	c := r.Comparison
	if c == nil {
		return
	}
	d.heading(t.f("compare.title", t.rangeLabel(c.Range)))
	rows := make([][]string, len(r.Figures))
	for i, f := range r.Figures {
		text, _ := changeText(t, f)
		rows[i] = []string{
			t.s("fig." + f.ID), figureValue(t, f.Compared, f.Kind),
			figureValue(t, f.Base, f.Kind), text,
		}
	}
	d.grid([]float64{0, 0.4, 0.6, 0.8, 1}, []string{
		t.s("figure"), t.s("this.period"),
		t.s("comparison"), t.s("change"),
	}, rows)
	d.y += gapS
	note := t.s("compare.whole")
	if c.Matched {
		note = t.plural(c.MatchedDays, "compare.matched")
	}
	d.paragraph(note, sizeSmall, colorMuted)
	d.y += sectionGap
}

func qualitySection(d *doc, r report.Report) {
	t := d.t
	s := r.Summary
	d.heading(t.s("quality"))
	d.paragraph(t.f("quality.days", s.CompleteDays, s.Days), sizeBody, colorBody)
	if len(r.MissingDays) > 0 {
		d.paragraph(t.s("quality.missing")+" "+dayList(t, r.MissingDays, s.Days-s.CompleteDays),
			sizeBody, colorBody)
	}
	if s.FallbackDays > 0 {
		d.paragraph(t.plural(s.FallbackDays, "note.fallback"), sizeBody, colorBody)
	}
	d.y += gapM
	rows := make([][]string, len(r.Quality))
	for i, q := range r.Quality {
		rows[i] = []string{
			q.Label, t.plural(q.MissingDays, "days"),
			t.plural(q.EstimatedDays, "days"),
		}
	}
	d.grid([]float64{0, 0.5, 0.75, 1}, []string{
		t.s("meter"), t.s("missing"),
		t.s("estimated"),
	}, rows)
	d.y += sectionGap
}

// dayList names the first missing days and how many more there are.
func dayList(t tr, days []time.Time, total int) string {
	n := min(len(days), maxListedDays)
	names := make([]string, n)
	for i, day := range days[:n] {
		names[i] = t.shortDay(day)
	}
	s := strings.Join(names, ", ")
	if rest := total - n; rest > 0 {
		s += " " + t.f("and.more", rest)
	}
	return s
}
