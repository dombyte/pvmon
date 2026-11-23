// Package pdf renders a report as an A4 PDF (spec 12): a summary page with key figures,
// energy charts and where the energy came from and went, the sections the site has
// (battery, PV sources, loadpoints, load profile), patterns, comparison and data quality,
// a compact table and a last page on how the report is calculated. German or English.
// Charts are vectors in a print palette independent of the UI theme and readable in
// greyscale. Pure Go (works in the scratch image); fonts are embedded (Inter, SIL Open
// Font License, see fonts/OFL.txt).
package pdf

import (
	_ "embed" // fonts
	"fmt"
	"io"
	"time"

	"github.com/signintech/gopdf"

	"github.com/dombyte/pvmon/internal/report"
)

//go:embed fonts/Inter-Regular.ttf
var interRegular []byte

//go:embed fonts/Inter-SemiBold.ttf
var interSemiBold []byte

//go:embed fonts/Inter-Bold.ttf
var interBold []byte

// Meta is the information that does not come from the report.
type Meta struct {
	// Site is the site name; Version the app version.
	Site, Version string
	// Generated is the creation time in the server time zone.
	Generated time.Time
	// Lang is "de" or "en" (anything else is English).
	Lang string
}

// Write renders r to w.
func Write(w io.Writer, r report.Report, m Meta) error {
	d, err := newDoc(m)
	if err != nil {
		return err
	}
	summaryPage(d, r, m)
	detailPages(d, r)
	tablePages(d, r)
	methodPage(d, r, m)
	footers(d, r, m)
	if d.err != nil {
		return fmt.Errorf("pdf: draw: %w", d.err)
	}
	b, err := d.pdf.GetBytesPdfReturnErr()
	if err != nil {
		return fmt.Errorf("pdf: compile: %w", err)
	}
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("pdf: write: %w", err)
	}
	return nil
}

// newDoc starts an A4 document with the embedded fonts.
func newDoc(m Meta) (*doc, error) {
	all, err := texts()
	if err != nil {
		return nil, err
	}
	t := newTr(m.Lang, all)
	p := &gopdf.GoPdf{}
	p.Start(gopdf.Config{Unit: gopdf.UnitPT, PageSize: *gopdf.PageSizeA4})
	p.SetInfo(gopdf.PdfInfo{
		Title: t.s("title"), Creator: "pvmon " + m.Version, CreationDate: m.Generated,
	})
	fonts := map[string][]byte{
		fontRegular: interRegular, fontSemiBold: interSemiBold, fontBold: interBold,
	}
	for name, data := range fonts {
		if err := p.AddTTFFontData(name, data); err != nil {
			return nil, fmt.Errorf("pdf: load font %s: %w", name, err)
		}
	}
	return &doc{pdf: p, t: t}, nil
}

// box is a rectangle in points from the upper-left corner.
type box struct {
	x, y, w, h float64
}

// doc wraps gopdf with a sticky error so drawing code stays linear: after the first
// failure every call is a no-op and Write reports that error. y is the flow cursor.
type doc struct {
	pdf *gopdf.GoPdf
	t   tr
	err error
	y   float64
}

// newPage starts a page and puts the cursor at the top margin.
func (d *doc) newPage() {
	d.pdf.AddPage()
	d.y = marginTop
}

// need starts a new page when h does not fit below the cursor.
func (d *doc) need(h float64) {
	if d.y+h > contentBotY {
		d.newPage()
	}
}

func (d *doc) fail(err error) {
	if d.err == nil && err != nil {
		d.err = err
	}
}

// font selects a face and size (pt) and the text color.
func (d *doc) font(face string, size float64, c rgb) {
	d.fail(d.pdf.SetFont(face, "", size))
	d.pdf.SetTextColor(c.r, c.g, c.b)
}

// text writes s into b with align (gopdf.Left|Right|Center plus Top|Middle|Bottom).
func (d *doc) text(b box, s string, align int) {
	if d.err != nil {
		return
	}
	d.pdf.SetXY(b.x, b.y)
	d.fail(d.pdf.CellWithOption(&gopdf.Rect{W: b.w, H: b.h}, s, gopdf.CellOption{Align: align}))
}

// width measures s in the current font.
func (d *doc) width(s string) float64 {
	w, err := d.pdf.MeasureTextWidth(s)
	d.fail(err)
	return w
}

// rect fills b with c.
func (d *doc) rect(b box, c rgb) {
	if b.w <= 0 || b.h <= 0 {
		return
	}
	d.pdf.SetFillColor(c.r, c.g, c.b)
	d.pdf.RectFromUpperLeftWithStyle(b.x, b.y, b.w, b.h, "F")
}

// line draws from (x1, y1) to (x2, y2) with the current stroke.
func (d *doc) line(x1, y1, x2, y2 float64) { d.pdf.Line(x1, y1, x2, y2) }

// stroke sets the stroke color and width.
func (d *doc) stroke(c rgb, width float64) {
	d.pdf.SetStrokeColor(c.r, c.g, c.b)
	d.pdf.SetLineWidth(width)
}

// rule draws a full-width line at y.
func (d *doc) rule(y float64, c rgb, width float64) {
	d.stroke(c, width)
	d.line(marginX, y, marginX+contentW, y)
}
