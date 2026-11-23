package pdf

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dombyte/pvmon/internal/report"
	"github.com/dombyte/pvmon/internal/util"
)

const (
	langDE    = "de"
	percent   = 100
	whPerKWh  = 1000
	noValue   = "–"
	minus     = "−"
	nbsp      = " "
	thousands = 3
	// apiDecimals: values are rounded like the JSON answer first, so the PDF and the
	// Reports page never differ in the last digit (spec 12).
	apiDecimals = 2
	oneDecimal  = 1
)

// tr translates texts and formats numbers and dates for one language.
type tr struct {
	de    bool
	texts map[string][2]string // key → {en, de}
}

func newTr(lang string, texts map[string][2]string) tr {
	return tr{de: lang == langDE, texts: texts}
}

// s returns the text of key; the key itself when it is unknown (a test checks all keys).
func (t tr) s(key string) string {
	v, ok := t.texts[key]
	if !ok {
		return key
	}
	if t.de {
		return v[1]
	}
	return v[0]
}

// f formats the text of key with args.
func (t tr) f(key string, args ...any) string { return fmt.Sprintf(t.s(key), args...) }

// number formats v with one decimal and thousands separators ("4,512.3" / "4.512,3").
func (t tr) number(v float64) string {
	s := strconv.FormatFloat(math.Abs(util.Round(util.Round(v, apiDecimals), oneDecimal)),
		'f', oneDecimal, 64)
	intPart, frac, _ := strings.Cut(s, ".")
	sep, dec := ",", "."
	if t.de {
		sep, dec = ".", ","
	}
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%thousands == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(c)
	}
	out := b.String() + dec + frac
	if v < 0 && out != "0"+dec+"0" {
		return minus + out
	}
	return out
}

// kwh formats Wh as "403,7 kWh"; "–" when missing.
func (t tr) kwh(wh *float64) string {
	if wh == nil {
		return noValue
	}
	return t.number(*wh/whPerKWh) + nbsp + "kWh"
}

// kwhOf formats a known Wh value.
func (t tr) kwhOf(wh float64) string { return t.kwh(&wh) }

// pct formats a 0..1 share as "76,2 %"; "–" when missing.
func (t tr) pct(share *float64) string {
	if share == nil {
		return noValue
	}
	return t.number(*share*percent) + nbsp + "%"
}

// watts formats a power as "1,2 kW" or "850,0 W".
func (t tr) watts(w float64) string {
	if math.Abs(w) >= whPerKWh {
		return t.number(w/whPerKWh) + nbsp + "kW"
	}
	return t.number(w) + nbsp + "W"
}

func (t tr) monthName(m time.Month) string {
	en := []string{
		"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct",
		"Nov", "Dec",
	}
	de := []string{
		"Jan.", "Feb.", "März", "Apr.", "Mai", "Juni", "Juli", "Aug.", "Sep.",
		"Okt.", "Nov.", "Dez.",
	}
	if t.de {
		return de[m-1]
	}
	return en[m-1]
}

// day is "1. Okt. 2026" / "1 Oct 2026".
func (t tr) day(d time.Time) string { return t.shortDay(d) + " " + strconv.Itoa(d.Year()) }

// shortDay is "1. Okt." / "1 Oct".
func (t tr) shortDay(d time.Time) string {
	if t.de {
		return strconv.Itoa(d.Day()) + ". " + t.monthName(d.Month())
	}
	return strconv.Itoa(d.Day()) + " " + t.monthName(d.Month())
}

// month is "Okt. 2026" / "Oct 2026".
func (t tr) month(d time.Time) string {
	return t.monthName(d.Month()) + " " + strconv.Itoa(d.Year())
}

// weekday is "Sa., 10. Okt." / "Sat 10 Oct".
func (t tr) weekday(d time.Time) string {
	en := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	de := []string{"So.,", "Mo.,", "Di.,", "Mi.,", "Do.,", "Fr.,", "Sa.,"}
	if t.de {
		return de[d.Weekday()] + " " + t.shortDay(d)
	}
	return en[d.Weekday()] + " " + t.shortDay(d)
}

// dateTime is "19.11.2026, 14:02" / "19 Nov 2026, 14:02".
func (t tr) dateTime(d time.Time) string {
	if t.de {
		return d.Format("02.01.2006, 15:04")
	}
	return t.day(d) + ", " + d.Format("15:04")
}

// rangeLabel names a range briefly: a whole month or year by name, otherwise the days.
func (t tr) rangeLabel(r report.Range) string {
	if r.From.Equal(r.To) {
		return t.day(r.From)
	}
	if wholeMonths(r) {
		if r.From.Month() == time.January && r.To.Month() == time.December {
			return strconv.Itoa(r.From.Year())
		}
		if r.From.Month() == r.To.Month() {
			return t.month(r.From)
		}
	}
	return t.day(r.From) + " – " + t.day(r.To)
}

// wholeMonths reports whether r starts on a first, ends on a last day, within one year.
func wholeMonths(r report.Range) bool {
	return r.From.Day() == 1 && r.To.AddDate(0, 0, 1).Day() == 1 &&
		r.From.Year() == r.To.Year()
}

// plural picks the singular or plural text of key ("<key>.one" / "<key>.many").
func (t tr) plural(n int, key string) string {
	if n == 1 {
		return t.f(key+".one", n)
	}
	return t.f(key+".many", n)
}
