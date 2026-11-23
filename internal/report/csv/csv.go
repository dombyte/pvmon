// Package csv writes a report's buckets as CSV: one row per day, month or year with the
// site quantities and the derived columns (this app's estimates end in _est_kwh). UTF-8,
// comma separator, dot decimals, ISO dates, values rounded to two decimals. A bucket
// without complete days has empty value cells (no data is not 0 kWh); columns of
// quantities the site does not have are left out.
package csv

import (
	stdcsv "encoding/csv"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/dombyte/pvmon/internal/report"
	"github.com/dombyte/pvmon/internal/util"
)

const (
	percent  = 100
	whPerKWh = 1000
	decimals = 2
)

// column is one CSV column: a header and the value of a bucket with data.
type column struct {
	header string
	value  func(report.Summary) *float64
}

func energyColumn(header string, field func(report.Energy) float64) column {
	return column{header, func(s report.Summary) *float64 {
		v := field(s.Total) / whPerKWh
		return &v
	}}
}

func whColumn(header string, field func(report.Summary) float64) column {
	return column{header, func(s report.Summary) *float64 {
		v := field(s) / whPerKWh
		return &v
	}}
}

func ratioColumn(header string, field func(report.Summary) *float64) column {
	return column{header, func(s report.Summary) *float64 {
		v := field(s)
		if v == nil {
			return nil
		}
		p := *v * percent
		return &p
	}}
}

// valueColumns lists the value columns of the site of l.
func valueColumns(l report.Layout) []column {
	var out []column
	if l.HasPV() {
		out = append(out, energyColumn("pv_kwh", func(e report.Energy) float64 { return e.PV }))
	}
	out = append(out,
		energyColumn("consumption_kwh", func(e report.Energy) float64 { return e.Consumption }),
		energyColumn("grid_import_kwh", func(e report.Energy) float64 { return e.Import }),
		energyColumn("grid_export_kwh", func(e report.Energy) float64 { return e.Export }))
	if l.HasBattery() {
		out = append(out,
			energyColumn("battery_charge_kwh", func(e report.Energy) float64 { return e.Charge }),
			energyColumn("battery_discharge_kwh",
				func(e report.Energy) float64 { return e.Discharge }),
			whColumn("battery_grid_charge_est_kwh",
				func(s report.Summary) float64 { return s.GridCharge }))
	}
	out = append(out,
		whColumn("consumption_solar_est_kwh",
			func(s report.Summary) float64 { return s.Sources.Solar }),
		ratioColumn("self_sufficiency_pct",
			func(s report.Summary) *float64 { return s.SelfSufficiency }))
	if l.HasPV() {
		out = append(out, ratioColumn("self_consumption_pct",
			func(s report.Summary) *float64 { return s.SelfConsumption }))
	}
	return out
}

// Write writes the buckets of r to w.
func Write(w io.Writer, r report.Report) error {
	cols := valueColumns(r.Layout)
	cw := stdcsv.NewWriter(w)
	header := []string{"period", "from", "to", "partial", "days", "complete_days"}
	for _, c := range cols {
		header = append(header, c.header)
	}
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("csv: write header: %w", err)
	}
	for _, b := range r.Buckets {
		if err := cw.Write(row(b, cols)); err != nil {
			return fmt.Errorf("csv: write %s: %w", b.Key, err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("csv: flush: %w", err)
	}
	return nil
}

func row(b report.Bucket, cols []column) []string {
	s := b.Summary
	out := []string{
		b.Key, b.From.Format(time.DateOnly), b.To.Format(time.DateOnly),
		strconv.FormatBool(b.Partial), strconv.Itoa(s.Days), strconv.Itoa(s.CompleteDays),
	}
	for _, c := range cols {
		out = append(out, cell(s, c))
	}
	return out
}

// cell formats one value; empty without data.
func cell(s report.Summary, c column) string {
	if s.CompleteDays == 0 {
		return ""
	}
	v := c.value(s)
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(util.Round(*v, decimals), 'f', decimals, 64)
}
