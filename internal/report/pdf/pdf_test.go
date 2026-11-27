package pdf

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/report"
)

func day(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

// sample builds a rich report: two PV sources, two batteries, loadpoints, a comparison
// and missing days.
func sample(from, to string) report.Report {
	l := report.Layout{
		PV:         []report.Meter{{ID: "roof", Label: "Dach", KWp: 9.8}, {ID: "dtu", Label: "Balkon"}},
		Batteries:  []report.Meter{{ID: "b1", Label: "Batterie 1"}, {ID: "b2", Label: "Batterie 2"}},
		Loadpoints: []report.Meter{{ID: "wb", Label: "Garage", Category: "wallbox"}, {ID: "hp", Label: "WP", Category: "heatpump"}},
	}
	mk := func(d time.Time, i int) report.Day {
		pv := float64(8000 + (i*3700)%14000)
		v := map[string]float64{
			report.KeyPV: pv, report.KeyConsumption: 11000, report.KeyImport: 4000,
			report.KeyExport: pv - 5000, report.KeyCharge: 3000, report.KeyDischarge: 2800,
			"pv:roof": pv * 0.8, "pv:dtu": pv * 0.2, "charge:b1": 2000, "charge:b2": 1000,
			"discharge:b1": 1900, "discharge:b2": 900, "lp:wb": 3000, "lp:hp": 2500,
		}
		var slots []report.Slot
		for s := range report.SlotsPerDay {
			slots = append(slots, report.Slot{
				At: d.Add(time.Duration(s) * 15 * time.Minute),
				Values: map[string]float64{
					report.KeyConsumption: 100 + float64(s%40)*5,
					report.KeyImport:      30, report.KeyCharge: 10, report.KeyDischarge: 20, "lp:wb": 20, "lp:hp": 15,
				},
			})
		}
		return report.Day{Date: d, Values: v, Slots: report.EstimateDay(slots, []string{"wb", "hp"})}
	}
	r := report.Range{From: day(from), To: day(to)}
	var days, base []report.Day
	for i := range r.Days() {
		if i%9 != 4 {
			days = append(days, mk(r.From.AddDate(0, 0, i), i))
		}
		base = append(base, mk(r.From.AddDate(-1, 0, i), i+3))
	}
	cmp, _ := report.CompareLastYear.Of(r)
	return report.Build(report.Input{
		Range: r, Granularity: report.AutoGranularity(r), Layout: l, Days: days,
		Compare:       &report.CompareInput{Mode: report.CompareLastYear, Range: cmp, Days: base},
		Quality:       []report.MeterQuality{{ID: "roof", Label: "Dach", MissingDays: 3}},
		TodayExcluded: true,
	})
}

func TestWrite(t *testing.T) {
	t.Parallel()
	gen := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	for _, tc := range []struct{ lang, from, to string }{
		{"de", "2026-10-01", "2026-10-31"},
		{"en", "2026-01-01", "2026-10-31"},
		{"de", "2018-01-01", "2026-10-31"},
	} {
		var buf bytes.Buffer
		r := sample(tc.from, tc.to)
		require.NoError(t, Write(&buf, r, Meta{
			Site: "Home", Version: "1.0.0", Generated: gen,
			Lang: tc.lang,
		}))
		assert.True(t, bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")))
		writeSample(t, tc.lang+"-"+tc.from+".pdf", buf.Bytes())
	}
	one := sample("2026-10-01", "2026-10-01")
	one.Comparison.Matched, one.Comparison.MatchedDays = true, 0
	var buf bytes.Buffer
	require.NoError(t, Write(&buf, one, Meta{Site: "Home", Generated: gen, Lang: "en"}))
	writeSample(t, "en-one-day.pdf", buf.Bytes())
	grid := report.Build(report.Input{Range: report.Range{
		From: day("2026-06-01"),
		To:   day("2026-06-01"),
	}})
	require.NoError(t, Write(&bytes.Buffer{}, grid, Meta{Generated: gen}))
	require.Error(t, Write(failing{}, grid, Meta{Generated: gen}))
}

// writeSample keeps a rendered PDF for review when PVMON_PDF_OUT names a directory.
func writeSample(t *testing.T, name string, b []byte) {
	t.Helper()
	if dir := os.Getenv("PVMON_PDF_OUT"); dir != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), b, 0o600))
	}
}

func TestHeaderLines(t *testing.T) {
	t.Parallel()
	d, err := newDoc(Meta{})
	require.NoError(t, err)
	d.newPage()
	d.font(fontSemiBold, sizeSmall, hex(colorInk))
	assert.Equal(t, []string{"Charged"}, d.headerLines("Charged", 60))
	assert.Equal(t, []string{"Self-", "sufficiency"}, d.headerLines("Self-sufficiency", 60))
	assert.Equal(t, []string{"Grid", "import"}, d.headerLines("Grid import", 40))
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// TestTextKeys checks the texts and the number and date formats.
func TestTextKeys(t *testing.T) {
	t.Parallel()
	all, err := texts()
	require.NoError(t, err)
	checkKeysInSource(t, all)
	checkDynamicKeys(t, all)
	de, en := newTr("de", all), newTr("en", all)
	assert.Equal(t, "1.234,5", de.number(1234.5))
	assert.Equal(t, "−3,0", de.number(-3))
	assert.Equal(t, "0,0", de.number(-0.01))
	assert.Equal(t, "1,234.5", en.number(1234.5))
	assert.Equal(t, "unknown.key", de.s("unknown.key"))
	assert.Equal(t, "1. Okt. 2026", de.day(day("2026-10-01")))
	assert.Equal(t, "Okt. 2026", de.rangeLabel(report.Range{
		From: day("2026-10-01"),
		To:   day("2026-10-31"),
	}))
	assert.Equal(t, "2026", de.rangeLabel(report.Range{
		From: day("2026-01-01"),
		To:   day("2026-12-31"),
	}))
	assert.Equal(t, "1 Oct 2026 – 1 Mar 2027", en.rangeLabel(report.Range{
		From: day("2026-10-01"), To: day("2027-03-01"),
	}))
	assert.Equal(t, "850,0\u00a0W", de.watts(850))
	assert.Equal(t, "1,5\u00a0kW", de.watts(1500))
	assert.Equal(t, "–", de.pct(nil))
}

func checkKeysInSource(t *testing.T, all map[string][2]string) {
	t.Helper()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	re := regexp.MustCompile(`t\.(?:s|f|plural)\((?:[^"()]*, )?"([a-z_.]+)"`)
	for _, f := range files {
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			key := m[1]
			plural := hasKey(all, key+".one") && hasKey(all, key+".many")
			prefix := key[len(key)-1] == '.' // completed at runtime, e.g. "fig." + ID
			assert.True(t, hasKey(all, key) || plural || prefix, "%s: %q", f, key)
		}
	}
}

func checkDynamicKeys(t *testing.T, all map[string][2]string) {
	t.Helper()
	dynamic := []string{
		"chart.energy.day", "chart.energy.month", "chart.energy.year",
		"table.day", "table.month", "table.year", "cat.wallbox", "cat.heatpump", "cat.heater",
		"cat.other", "column.pv",
	}
	for _, f := range report.Build(report.Input{Layout: report.Layout{
		PV: []report.Meter{{}}, Batteries: []report.Meter{{}},
	}}).Figures {
		dynamic = append(dynamic, "fig."+f.ID)
	}
	for _, k := range dynamic {
		assert.True(t, hasKey(all, k), k)
	}
	for k, v := range all {
		assert.True(t, v[0] != "" && v[1] != "", "%s has both languages", k)
	}
}

func hasKey(m map[string][2]string, k string) bool {
	_, ok := m[k]
	return ok
}
