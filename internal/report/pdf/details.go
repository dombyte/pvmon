package pdf

import (
	"fmt"
	"strings"

	"github.com/dombyte/pvmon/internal/report"
)

const (
	profileH      = 120.0
	slotsPerTick  = 12 // a label every three hours
	maxListedDays = 12
	ratioDecimals = 2
)

// detailPages draws the sections the site has, flowing onto new pages.
func detailPages(d *doc, r report.Report) {
	d.pageTitle(d.t.s("details"))
	if r.Layout.HasBattery() {
		batterySection(d, r)
	}
	if r.PVSources != nil {
		pvSection(d, r.PVSources)
	}
	if r.Loadpoints != nil {
		loadpointSection(d, r.Loadpoints)
	}
	profileSection(d, r.Profile)
	patternsSection(d, r)
	comparisonSection(d, r)
	qualitySection(d, r)
}

// keyRows draws label/value rows in two columns.
func keyRows(d *doc, rows [][2]string) {
	d.grid([]float64{0, 0.6, 1}, []string{"", ""}, nil)
	d.y -= rowH // the empty header only draws the top rule
	for _, r := range rows {
		d.need(rowH)
		table{edges: []float64{0, 0.6, 1}, face: fontRegular, size: sizeBody, rowH: rowH}.
			row(d, d.y, r[:], []uint32{colorBody, colorInk})
		d.y += rowH
		d.rule(d.y, hex(colorRule), lineThin)
	}
	d.y += sectionGap
}

func ratio(t tr, v *float64) string {
	if v == nil {
		return t.f("from.days", report.MinBalanceDays)
	}
	return strings.Replace(fmt.Sprintf("%.*f", ratioDecimals, *v), ".", decimalSep(t), 1)
}

func decimalSep(t tr) string {
	if t.de {
		return ","
	}
	return "."
}

func batterySection(d *doc, r report.Report) {
	t := d.t
	s := r.Summary
	d.heading(t.s("battery"))
	diff := t.f("from.days", report.MinBalanceDays)
	if s.BalanceDiff != nil {
		diff = t.kwh(s.BalanceDiff) + " (" + t.pct(s.BalanceDiffShare) + ")"
	}
	keyRows(d, [][2]string{
		{t.s("fig.battery_charge"), t.kwhOf(s.Total.Charge)},
		{t.s("fig.battery_discharge"), t.kwhOf(s.Total.Discharge)},
		{t.s("grid.charge") + " ≈", t.kwhOf(s.GridCharge)},
		{t.s("battery.ratio"), ratio(t, s.BatteryRatio)},
		{t.s("balance.diff"), diff},
	})
	if r.Batteries == nil {
		return
	}
	rows := make([][]string, len(r.Batteries.Meters))
	for i, m := range r.Batteries.Meters {
		rows[i] = []string{m.Label, t.kwhOf(m.In), t.kwhOf(m.Out), ratio(t, m.Ratio)}
	}
	d.grid([]float64{0, 0.4, 0.6, 0.8, 1}, []string{
		t.s("battery"),
		t.s("fig.battery_charge"), t.s("fig.battery_discharge"), t.s("battery.ratio"),
	}, rows)
	d.y += sectionGap
}

func pvSection(d *doc, s *report.Section) {
	t := d.t
	d.heading(t.s("pv.sources"))
	rows := make([][]string, len(s.Meters))
	for i, m := range s.Meters {
		yield := noValue
		if m.SpecificYield != nil {
			yield = t.number(*m.SpecificYield) + nbsp + "kWh/kWp"
		}
		rows[i] = []string{m.Label, t.kwhOf(m.Out), t.pct(m.Share), yield}
	}
	d.grid([]float64{0, 0.4, 0.6, 0.75, 1}, []string{
		t.s("source"), t.s("fig.pv"),
		t.s("share"), t.s("specific.yield"),
	}, rows)
	d.y += sectionGap
}

func loadpointSection(d *doc, lp *report.Loadpoints) {
	t := d.t
	d.heading(t.s("loadpoints"))
	var rows [][]string
	for _, m := range lp.Meters {
		rows = append(rows, []string{
			m.Label, t.s("cat." + m.Category), t.kwhOf(m.In),
			t.kwhOf(m.Solar), t.pct(m.SolarShare),
		})
	}
	if len(lp.Categories) == len(lp.Meters) {
		lp.Categories = nil // one loadpoint per category: the sums repeat the rows
	}
	for _, c := range lp.Categories {
		rows = append(rows, []string{
			t.s("sum"), t.s("cat." + c.Category), t.kwhOf(c.In),
			t.kwhOf(c.Solar), t.pct(c.SolarShare),
		})
	}
	d.grid([]float64{0, 0.3, 0.5, 0.67, 0.84, 1}, []string{
		t.s("loadpoint"),
		t.s("category"), t.s("energy"), t.s("solar") + " ≈", t.s("solar.share") + " ≈",
	}, rows)
	d.y += sectionGap
}

func profileSection(d *doc, p report.Profile) {
	t := d.t
	d.need(headingH + profileH + captionH + sectionGap)
	d.heading(t.s("profile"))
	values := make([]*float64, report.SlotsPerDay)
	ticks := make([]string, report.SlotsPerDay)
	for i, v := range p.Mean {
		if v != nil {
			kw := *v / whPerKWh
			values[i] = &kw
		}
		if i%slotsPerTick == 0 {
			ticks[i] = fmt.Sprintf("%02d:00", i/(report.SlotsPerDay/hoursPerDay))
		}
	}
	d.bars(barChart{
		area: box{marginX, d.y, contentW, profileH}, ticks: ticks,
		series: []barSeries{{colorConsumption, values}}, suffix: "kW",
	})
	d.y += profileH + gapS
	if p.Peak != nil {
		d.paragraph(t.f("profile.peak", t.watts(*p.Peak), t.dateTime(p.PeakAt)), sizeSmall,
			colorMuted)
	}
	d.y += sectionGap
}

const hoursPerDay = 24
