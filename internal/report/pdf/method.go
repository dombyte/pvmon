package pdf

import (
	"github.com/dombyte/pvmon/internal/report"
)

// methodPage explains the data, the standard figures, the estimates and the rules; it
// is kept in sync with internal/report (spec 12).
func methodPage(d *doc, r report.Report, m Meta) {
	t := d.t
	d.pageTitle(t.s("method"))
	sections := []struct {
		title string
		items []string
	}{
		{t.s("method.data"), []string{
			t.s("method.data.counters"), t.s("method.data.days"),
			t.f("method.data.zone", m.Generated.Location().String()),
		}},
		{t.s("method.standard"), []string{
			t.s("method.self_sufficiency"), t.s("method.self_consumption"),
			t.s("method.ratio"), t.s("method.balance"),
		}},
		{t.s("method.estimates"), []string{
			t.s("method.solar_share"), t.s("method.grid_charge"), t.s("method.loadpoint"),
			t.s("method.sources"), t.s("method.fallback"),
		}},
		{t.s("method.rules"), []string{
			t.s("method.missing"), t.f("method.stable", report.MinBalanceDays),
			t.s("method.compare"), t.s("method.today"), t.s("method.rounding"),
		}},
	}
	for _, s := range sections {
		d.heading(s.title)
		for _, item := range s.items {
			d.paragraph("• "+item, sizeBody, colorBody)
			d.y += gapXS
		}
		d.y += gapL
	}
	_ = r
}

// footers writes the site, the period and the page number on every page.
func footers(d *doc, r report.Report, m Meta) {
	t := d.t
	n := d.pdf.GetNumberOfPages()
	for i := 1; i <= n; i++ {
		d.fail(d.pdf.SetPage(i))
		d.rule(footerY-gapS, hex(colorRule), lineThin)
		d.font(fontRegular, sizeSmall, hex(colorMuted))
		left := "pvmon · " + m.Site + " · " + t.rangeLabel(r.Range)
		d.text(box{marginX, footerY, contentW, captionH}, left, alignLeft)
		d.text(box{marginX, footerY, contentW, captionH}, t.f("page", i, n), alignRight)
	}
}
