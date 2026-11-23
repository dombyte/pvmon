package pdf

import "github.com/dombyte/pvmon/internal/report"

func splits(d *doc, r report.Report) {
	t := d.t
	s := r.Summary
	src := []splitPart{{colorPV, t.s("split.solar"), s.Sources.SolarShare, s.Sources.Solar}}
	if r.Layout.HasBattery() {
		src = append(src, splitPart{
			colorBattery, t.s("split.battery"), s.Sources.BatteryShare,
			s.Sources.Battery,
		})
	}
	src = append(src, splitPart{
		colorImport, t.s("split.grid"), s.Sources.GridShare,
		s.Sources.Grid,
	})
	splitSection(d, t.s("split.sources"), src)
	if !r.Layout.HasPV() {
		return
	}
	pv := []splitPart{{
		colorConsumption, t.s("split.direct"), s.PVSplit.SolarShare,
		s.PVSplit.Solar,
	}}
	if r.Layout.HasBattery() {
		pv = append(pv, splitPart{
			colorBattery, t.s("split.charged"), s.PVSplit.BatteryShare,
			s.PVSplit.Battery,
		})
	}
	pv = append(pv, splitPart{
		colorExport, t.s("split.export"), s.PVSplit.GridShare,
		s.PVSplit.Grid,
	})
	splitSection(d, t.s("split.pv"), pv)
}

// splitSection draws a titled 100 % bar with a label, kWh and share per part below.
func splitSection(d *doc, title string, parts []splitPart) {
	d.need(headingH + splitBarH + half*captionH + sectionGap)
	d.font(fontSemiBold, sizeHeading, hex(colorInk))
	d.text(box{marginX, d.y, contentW, headingH}, title+" ≈", alignLeft)
	d.y += headingH + gapM
	d.splitBar(box{marginX, d.y, contentW, splitBarH}, parts)
	d.y += splitBarH + gapS
	w := contentW / float64(len(parts))
	for i, p := range parts {
		x := marginX + float64(i)*w
		d.rect(box{x, d.y + gapS, legendSwatch, legendSwatch}, hex(p.color))
		d.font(fontSemiBold, sizeSmall, hex(colorInk))
		d.text(box{x + legendSwatch + gapS, d.y, w, captionH}, p.label, alignLeft)
		d.font(fontRegular, sizeSmall, hex(colorMuted))
		d.text(box{x + legendSwatch + gapS, d.y + captionH, w, captionH},
			d.t.kwhOf(p.wh)+" · "+d.t.pct(p.share), alignLeft)
	}
	d.y += half*captionH + sectionGap
}

// summaryNotes explains what the figures leave out.
func summaryNotes(d *doc, r report.Report) {
	t := d.t
	s := r.Summary
	if r.TodayExcluded {
		d.paragraph(t.s("note.today"), sizeSmall, colorMuted)
	}
	if !s.Complete() {
		d.paragraph(t.f("note.incomplete", s.CompleteDays, s.Days), sizeSmall, colorMuted)
	}
	if s.FallbackDays > 0 {
		d.paragraph(t.plural(s.FallbackDays, "note.fallback"), sizeSmall, colorMuted)
	}
	d.paragraph(t.s("note.estimates"), sizeSmall, colorMuted)
}
