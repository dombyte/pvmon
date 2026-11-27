package pdf

import (
	"strconv"

	"github.com/dombyte/pvmon/internal/report"
)

// column is one value column of the table.
type column struct {
	key string
	get func(report.Summary) string
}

// tablePages lists every bucket with the site quantities, continued on further pages.
func tablePages(d *doc, r report.Report) {
	if len(r.Buckets) < minBuckets {
		return // the summary tiles already show the only row
	}
	t := d.t
	d.pageTitle(t.s("table." + r.Granularity.String()))
	cols := columns(t, r.Layout)
	header := []string{t.s("period")}
	for _, c := range cols {
		header = append(header, t.s(c.key))
	}
	rows := make([][]string, len(r.Buckets))
	for i, b := range r.Buckets {
		rows[i] = []string{bucketLabel(t, b, r.Granularity)}
		for _, c := range cols {
			v := noValue
			if b.Summary.CompleteDays > 0 {
				v = c.get(b.Summary)
			}
			rows[i] = append(rows[i], v)
		}
	}
	d.paragraph(t.s("table.unit"), sizeSmall, colorMuted)
	d.y += gapS
	d.gridSmall(edges(len(cols)), header, rows)
}

// columns lists the quantities the site has.
func columns(t tr, l report.Layout) []column {
	energy := func(key string, f func(report.Energy) float64) column {
		return column{key, func(s report.Summary) string { return t.number(f(s.Total) / whPerKWh) }}
	}
	var cols []column
	if l.HasPV() {
		// the short label leaves a gap to "Consumption"
		cols = append(cols, energy("column.pv", func(e report.Energy) float64 { return e.PV }))
	}
	cols = append(cols,
		energy("fig.consumption", func(e report.Energy) float64 { return e.Consumption }),
		energy("fig.grid_import", func(e report.Energy) float64 { return e.Import }),
		energy("fig.grid_export", func(e report.Energy) float64 { return e.Export }))
	if l.HasBattery() {
		cols = append(cols,
			energy("fig.battery_charge", func(e report.Energy) float64 { return e.Charge }),
			energy("fig.battery_discharge", func(e report.Energy) float64 { return e.Discharge }))
	}
	return append(cols, column{"fig.self_sufficiency", func(s report.Summary) string {
		return t.pct(s.SelfSufficiency)
	}})
}

// edges gives the period column 16 % and splits the rest evenly.
func edges(n int) []float64 {
	const first = 0.16
	out := []float64{0, first}
	for i := 1; i <= n; i++ {
		out = append(out, first+(1-first)*float64(i)/float64(n))
	}
	return out
}

// bucketLabel names a bucket; months and years cut by the range or with missing days are
// marked.
func bucketLabel(t tr, b report.Bucket, g report.Granularity) string {
	var s string
	switch g {
	case report.ByYear:
		s = b.Key
	case report.ByMonth:
		s = t.month(b.From)
	default:
		return t.weekday(b.From)
	}
	if !b.Summary.Complete() || b.Partial {
		s += " (" + strconv.Itoa(b.Summary.CompleteDays) + "/" + strconv.Itoa(b.Summary.Days) +
			")"
	}
	return s
}

// gridSmall is grid in the small size with a repeated header on every page.
func (d *doc) gridSmall(edges []float64, header []string, rows [][]string) {
	t := table{edges: edges, face: fontSemiBold, size: sizeSmall, rowH: rowH}
	head := func() {
		t.header(d, header)
		t.face = fontRegular
	}
	head()
	for _, r := range rows {
		if d.y+rowH > contentBotY {
			d.newPage()
			head()
		}
		t.row(d, d.y, r, nil)
		d.y += rowH
		d.rule(d.y, hex(colorRule), lineThin)
	}
}
