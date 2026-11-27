package pdf

import (
	"math"
	"strconv"
	"strings"
)

// Chart geometry and scale.
const (
	axisW        = 34.0 // y labels left of the plot
	tickH        = 13.0 // x labels below the plot
	gridLines    = 4
	barGapShare  = 0.25 // part of a slot left empty between groups
	splitGap     = 1.5
	decade       = 10.0
	legendGap    = 12.0
	legendTextGp = 4.0
)

// niceMax rounds v up to 1, 2, 2.5 or 5 times a power of ten (at least 1).
func niceMax(v float64) float64 {
	if v <= 1 {
		return 1
	}
	exp := math.Pow(decade, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if m*exp >= v {
			return m * exp
		}
	}
	return decade * exp
}

// barSeries is one series of a bar chart; nil values are slots without data.
type barSeries struct {
	color  uint32
	values []*float64
}

// maxOf returns the largest value of all series (0 without values).
func maxOf(ss ...barSeries) float64 {
	m := 0.0
	for _, s := range ss {
		for _, v := range s.values {
			if v != nil && *v > m {
				m = *v
			}
		}
	}
	return m
}

// barChart is a grouped bar chart, y from 0 to top.
type barChart struct {
	area   box
	ticks  []string
	series []barSeries
	top    float64 // axis maximum; 0 = niceMax of the data
	suffix string  // appended to the top axis label ("kWh", "%")
}

// bars draws c.
func (d *doc) bars(c barChart) {
	top := c.top
	if top <= 0 {
		top = niceMax(maxOf(c.series...))
	}
	plot := box{c.area.x + axisW, c.area.y, c.area.w - axisW, c.area.h - tickH}
	d.yAxis(plot, top, c.suffix)
	slot := plot.w / float64(len(c.ticks))
	barW := slot * (1 - barGapShare) / float64(len(c.series))
	for i := range c.ticks {
		x := plot.x + float64(i)*slot + slot*barGapShare/half
		for j, s := range c.series {
			if v := s.values[i]; v != nil && *v > 0 {
				h := math.Min(*v, top) / top * plot.h
				d.rect(box{x + float64(j)*barW, plot.y + plot.h - h, barW, h}, hex(s.color))
			}
		}
	}
	d.baseline(plot, plot.y+plot.h)
	d.xTicks(plot, c.ticks, plot.y+plot.h)
}

// yAxis draws the grid lines and their labels.
func (d *doc) yAxis(plot box, top float64, suffix string) {
	d.font(fontRegular, sizeSmall, hex(colorMuted))
	d.stroke(hex(colorGrid), lineThin)
	for i := range gridLines + 1 {
		v := top * float64(i) / gridLines
		y := plot.y + plot.h - plot.h*float64(i)/gridLines
		if i > 0 {
			d.line(plot.x, y, plot.x+plot.w, y)
		}
		label := d.axisNumber(v)
		if i == gridLines && suffix != "" {
			label += " " + suffix
		}
		d.text(box{plot.x - axisW - tileGap, y - tickH/half, axisW + tileGap - gapS + 1, tickH},
			label, alignRight)
	}
}

// axisNumber formats an axis value without needless decimals.
func (d *doc) axisNumber(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if d.t.de {
		s = strings.ReplaceAll(s, ".", ",")
	}
	return s
}

// baseline draws the x axis at y.
func (d *doc) baseline(plot box, y float64) {
	d.stroke(hex(colorMuted), lineThin)
	d.line(plot.x, y, plot.x+plot.w, y)
}

// xTicks writes the non-empty tick labels below y, centered on their slots.
func (d *doc) xTicks(plot box, ticks []string, y float64) {
	d.font(fontRegular, sizeSmall, hex(colorMuted))
	slot := plot.w / float64(len(ticks))
	lastRight := math.Inf(-1)
	for i, t := range ticks {
		if t == "" {
			continue
		}
		cx := plot.x + (float64(i)+center)*slot
		w := d.width(t) + legendTextGp
		if cx-w/half < lastRight { // would overlap the previous label
			continue
		}
		d.text(box{cx - w/half, y + 1, w, tickH}, t, alignCenter)
		lastRight = cx + w/half
	}
}

// divergingChart draws up above and down below a center line on a shared scale.
func (d *doc) divergingChart(area box, ticks []string, up, down barSeries) {
	top := niceMax(maxOf(up, down))
	halfH := (area.h - tickH) / half
	plot := box{area.x + axisW, area.y, area.w - axisW, halfH * half}
	mid := plot.y + halfH
	d.font(fontRegular, sizeSmall, hex(colorMuted))
	for _, l := range []struct {
		y float64
		s string
	}{{plot.y, d.axisNumber(top)}, {mid, "0"}, {plot.y + plot.h, d.axisNumber(top)}} {
		d.text(box{area.x, l.y - tickH/half, axisW - gapM, tickH}, l.s, alignRight)
	}
	slot := plot.w / float64(len(ticks))
	w := slot * (1 - barGapShare)
	for i := range ticks {
		x := plot.x + float64(i)*slot + slot*barGapShare/half
		if v := up.values[i]; v != nil {
			h := *v / top * halfH
			d.rect(box{x, mid - h, w, h}, hex(up.color))
		}
		if v := down.values[i]; v != nil {
			d.rect(box{x, mid, w, *v / top * halfH}, hex(down.color))
		}
	}
	d.baseline(plot, mid)
	d.xTicks(plot, ticks, plot.y+plot.h)
}

// legendItem is a colored swatch with a label.
type legendItem struct {
	color uint32
	label string
}

// legend draws items right-aligned ending at right, vertically centered on y.
func (d *doc) legend(right, y float64, items []legendItem) {
	d.font(fontRegular, sizeSmall, hex(colorMuted))
	x := right
	for i := len(items) - 1; i >= 0; i-- {
		w := d.width(items[i].label)
		x -= w
		d.text(box{x, y - tickH/half, w + 1, tickH}, items[i].label, alignLeft)
		x -= legendSwatch + legendTextGp
		d.rect(box{x, y - legendSwatch/half, legendSwatch, legendSwatch}, hex(items[i].color))
		x -= legendGap
	}
}

// splitPart is one segment of a 100 % bar.
type splitPart struct {
	color uint32
	label string
	share *float64
	wh    float64
}

// splitBar draws a 100 % bar of parts; an empty split is a gray bar.
func (d *doc) splitBar(b box, parts []splitPart) {
	if parts[0].share == nil {
		d.rect(b, hex(colorPanel))
		return
	}
	x := b.x
	usable := b.w - splitGap*float64(len(parts)-1)
	for _, p := range parts {
		w := *p.share * usable
		d.rect(box{x, b.y, w, b.h}, hex(p.color))
		x += w + splitGap
	}
}

// table is a row layout: column edges as shares of the content width; the first column
// is left-aligned, the others right-aligned.
type table struct {
	edges      []float64
	face       string
	size, rowH float64
}

// row writes cells at y; colors per cell (nil = ink).
func (t table) row(d *doc, y float64, cells []string, colors []uint32) {
	for i, s := range cells {
		color := uint32(colorInk)
		if colors != nil {
			color = colors[i]
		}
		d.font(t.face, t.size, hex(color))
		x0, x1 := marginX+t.edges[i]*contentW, marginX+t.edges[i+1]*contentW
		align := alignRight
		if i == 0 {
			align = alignLeft
		}
		d.text(box{x0, y, x1 - x0, t.rowH}, s, align)
	}
}

// header writes the bold header row with its rule at the cursor. A label wider than its
// column breaks at a space or after a hyphen; all labels then end on the same line.
func (t table) header(d *doc, cells []string) {
	t.face = fontSemiBold
	d.font(t.face, t.size, hex(colorInk))
	lines := make([][]string, len(cells))
	deepest := 1
	for i, s := range cells {
		lines[i] = d.headerLines(s, (t.edges[i+1]-t.edges[i])*contentW-gapL)
		deepest = max(deepest, len(lines[i]))
	}
	for n := range deepest {
		row := make([]string, len(cells))
		for i, l := range lines {
			if k := n - deepest + len(l); k >= 0 {
				row[i] = l[k]
			}
		}
		t.row(d, d.y+float64(n)*noteLineH, row, nil)
	}
	d.y += t.rowH + float64(deepest-1)*noteLineH
	d.rule(d.y, hex(colorInk), lineThin)
}

// headerLines splits a label that does not fit into maxW in the current font.
func (d *doc) headerLines(s string, maxW float64) []string {
	if s == "" || d.width(s) <= maxW {
		return []string{s}
	}
	lines := d.wrap(strings.ReplaceAll(s, "-", "- "), maxW)
	for i := range lines {
		lines[i] = strings.ReplaceAll(lines[i], "- ", "-")
	}
	return lines
}

// keep starts a new page unless a section heading and a table of n rows fit.
func (d *doc) keep(n int) {
	d.need(headingH + gapM + rowH*float64(n+1))
}

// grid draws a table with a header row and returns nothing; rows break onto new pages.
func (d *doc) grid(edges []float64, header []string, rows [][]string) {
	t := table{edges: edges, face: fontSemiBold, size: sizeBody, rowH: rowH}
	d.need(rowH * half)
	t.header(d, header)
	t.face = fontRegular
	for _, r := range rows {
		d.need(rowH)
		t.row(d, d.y, r, nil)
		d.y += rowH
		d.rule(d.y, hex(colorRule), lineThin)
	}
}

// wrap breaks s at spaces into lines no wider than maxW in the current font.
func (d *doc) wrap(s string, maxW float64) []string {
	var lines []string
	cur := ""
	// Split on plain spaces only: a no-break space ("100 %") keeps its words together.
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' }) {
		next := w
		if cur != "" {
			next = cur + " " + w
		}
		if cur != "" && d.width(next) > maxW {
			lines = append(lines, cur)
			next = w
		}
		cur = next
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// paragraph writes s wrapped to the content width at the cursor in the given style.
func (d *doc) paragraph(s string, size float64, c uint32) {
	d.font(fontRegular, size, hex(c))
	for _, l := range d.wrap(s, contentW) {
		d.need(noteLineH)
		d.font(fontRegular, size, hex(c))
		d.text(box{marginX, d.y, contentW, noteLineH}, l, alignLeft)
		d.y += noteLineH
	}
}

// heading writes a section heading at the cursor.
func (d *doc) heading(s string) {
	d.need(headingH + rowH*half)
	d.font(fontSemiBold, sizeHeading, hex(colorInk))
	d.text(box{marginX, d.y, contentW, headingH}, s, alignLeft)
	d.y += headingH + gapM
}

// pageTitle starts a page with a title and a rule.
func (d *doc) pageTitle(s string) {
	d.newPage()
	d.font(fontBold, sizePage, hex(colorInk))
	d.text(box{marginX, d.y, contentW, titleRuleY - gapS}, s, alignLeft)
	d.rule(d.y+titleRuleY, hex(colorInk), lineRule)
	d.y += titleRuleY + sectionGap
}
