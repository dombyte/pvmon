package pdf

import (
	"strconv"

	"github.com/dombyte/pvmon/internal/report"
	"github.com/dombyte/pvmon/internal/util"
)

// Summary page geometry.
const (
	tileCols    = 4
	chartH      = 125.0
	gridChartH  = 90.0
	splitBarH   = 14.0
	headerLineH = 15.0
	maxTicks    = 12
	// minBuckets is the number of periods from which charts and the table say more than
	// the key figures.
	minBuckets = 2
)

func summaryPage(d *doc, r report.Report, m Meta) {
	d.newPage()
	header(d, r, m)
	keyFigures(d, r)
	if len(r.Buckets) >= minBuckets {
		energyChart(d, r)
		gridChart(d, r)
	}
	splits(d, r)
	summaryNotes(d, r)
}

func header(d *doc, r report.Report, m Meta) {
	t := d.t
	d.font(fontBold, sizeTitle, hex(colorInk))
	d.text(box{marginX, d.y, contentW, titleRuleY}, t.s("title"), alignLeft)
	d.y += titleRuleY
	d.font(fontSemiBold, sizeHeading, hex(colorBrand))
	d.text(box{marginX, d.y, contentW, headerLineH}, t.rangeLabel(r.Range), alignLeft)
	d.y += headerLineH + gapS
	d.font(fontRegular, sizeSmall, hex(colorMuted))
	line := m.Site + " · " + t.f("generated", t.dateTime(m.Generated))
	if c := r.Comparison; c != nil {
		line += " · " + t.f("compared.with", t.rangeLabel(c.Range))
	}
	d.text(box{marginX, d.y, contentW, captionH}, line, alignLeft)
	d.y += captionH + gapM
	d.rule(d.y, hex(colorInk), lineRule)
	d.y += sectionGap
}

// keyFigures draws the figures as tiles, tileCols per row.
func keyFigures(d *doc, r report.Report) {
	w := (contentW - tileGap*(tileCols-1)) / tileCols
	for i, f := range r.Figures {
		col := i % tileCols
		if col == 0 && i > 0 {
			d.y += tileH + tileGap
		}
		b := box{marginX + float64(col)*(w+tileGap), d.y, w, tileH}
		drawTile(d, b, f, comparedWith(r.Comparison))
	}
	d.y += tileH + sectionGap
}

// comparedWith returns c, or nil when the two periods have no day to compare.
func comparedWith(c *report.Comparison) *report.Comparison {
	if c == nil || (c.Matched && c.MatchedDays == 0) {
		return nil
	}
	return c
}

func drawTile(d *doc, b box, f report.Figure, c *report.Comparison) {
	t := d.t
	d.rect(b, hex(colorPanel))
	inner := box{b.x + tilePad, b.y + tilePad, b.w - half*tilePad, sizeSmall + gapS}
	d.font(fontRegular, sizeSmall, hex(colorMuted))
	d.text(inner, t.s("fig."+f.ID), alignLeft)
	inner.y += inner.h + gapXS
	d.font(fontSemiBold, sizeTile, hex(colorInk))
	d.text(box{inner.x, inner.y, inner.w, sizeTile + gapS}, figureValue(t, f.Value, f.Kind),
		alignLeft)
	if c == nil {
		return
	}
	text, color := changeText(t, f)
	d.font(fontRegular, sizeSmall, hex(color))
	d.text(box{inner.x, b.y + b.h - tilePad - sizeSmall, inner.w, sizeSmall}, text, alignLeft)
}

// figureValue formats a figure: energy in kWh, ratios in percent.
func figureValue(t tr, v *float64, k report.FigureKind) string {
	if k == report.KindRatio {
		return t.pct(v)
	}
	return t.kwh(v)
}

// changeText is "+12,3 % ggü. Vergleich" (energy) or "+4,0 Pkt." (ratios), colored by
// whether the change is an improvement.
func changeText(t tr, f report.Figure) (string, uint32) {
	if f.Change == nil {
		return t.s("change.none"), colorMuted
	}
	v, unit := *f.Change*percent, t.s("points")
	if f.Kind == report.KindEnergy {
		if f.ChangePct == nil {
			return t.s("change.none"), colorMuted
		}
		v, unit = *f.ChangePct*percent, "%"
	}
	v = util.Round(util.Round(v, apiDecimals), oneDecimal) // the digits shown
	sign := "+"
	if v < 0 {
		sign = ""
	}
	return sign + t.number(v) + nbsp + unit, changeColor(f.Better, v)
}

func changeColor(better report.Direction, v float64) uint32 {
	switch {
	case v == 0 || better == report.Neutral:
		return colorMuted
	case (v > 0) == (better == report.HigherIsBetter):
		return colorBetter
	default:
		return colorWorse
	}
}

// ticks labels the buckets without crowding the axis.
func ticks(t tr, r report.Report) []string {
	n := len(r.Buckets)
	step := (n + maxTicks - 1) / maxTicks
	out := make([]string, n)
	for i, b := range r.Buckets {
		if i%step != 0 {
			continue
		}
		switch r.Granularity {
		case report.ByYear:
			out[i] = b.Key
		case report.ByMonth:
			out[i] = t.monthName(b.From.Month())
		default:
			out[i] = strconv.Itoa(b.From.Day())
		}
	}
	return out
}

// bucketKWh reads one energy per bucket in kWh; nil for buckets without complete days.
func bucketKWh(r report.Report, get func(report.Energy) float64) []*float64 {
	out := make([]*float64, len(r.Buckets))
	for i, b := range r.Buckets {
		if b.Summary.CompleteDays > 0 {
			v := get(b.Summary.Total) / whPerKWh
			out[i] = &v
		}
	}
	return out
}

func energyChart(d *doc, r report.Report) {
	t := d.t
	series := []barSeries{}
	items := []legendItem{}
	if r.Layout.HasPV() {
		series = append(series, barSeries{colorPV, bucketKWh(r, func(e report.Energy) float64 {
			return e.PV
		})})
		items = append(items, legendItem{colorPV, t.s("fig.pv")})
	}
	series = append(series, barSeries{
		colorConsumption,
		bucketKWh(r, func(e report.Energy) float64 { return e.Consumption }),
	})
	items = append(items, legendItem{colorConsumption, t.s("fig.consumption")})
	chartTitle(d, t.s("chart.energy."+r.Granularity.String()), items)
	d.bars(barChart{
		area: box{marginX, d.y, contentW, chartH}, ticks: ticks(t, r),
		series: series, suffix: "kWh",
	})
	d.y += chartH + sectionGap
}

func gridChart(d *doc, r report.Report) {
	t := d.t
	chartTitle(d, t.s("chart.grid"), []legendItem{
		{colorExport, t.s("fig.grid_export")}, {colorImport, t.s("fig.grid_import")},
	})
	d.divergingChart(box{marginX, d.y, contentW, gridChartH}, ticks(t, r),
		barSeries{colorExport, bucketKWh(r, func(e report.Energy) float64 { return e.Export })},
		barSeries{colorImport, bucketKWh(r, func(e report.Energy) float64 { return e.Import })})
	d.y += gridChartH + sectionGap
}

func chartTitle(d *doc, title string, items []legendItem) {
	d.need(headingH + chartH)
	d.font(fontSemiBold, sizeHeading, hex(colorInk))
	d.text(box{marginX, d.y, contentW, headingH}, title, alignLeft)
	d.legend(marginX+contentW, d.y+headingH/half, items)
	d.y += headingH + gapL
}
