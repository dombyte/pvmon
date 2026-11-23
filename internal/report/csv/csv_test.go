package csv

import (
	"bytes"
	"errors"
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

func TestWrite(t *testing.T) {
	t.Parallel()
	l := report.Layout{PV: []report.Meter{{ID: "pv"}}, Batteries: []report.Meter{{ID: "b"}}}
	d := report.Day{Date: day("2026-06-01"), Values: map[string]float64{
		report.KeyPV: 12345, report.KeyConsumption: 8000, report.KeyImport: 2000,
		report.KeyExport: 5000, report.KeyCharge: 1000, report.KeyDischarge: 900,
	}}
	r := report.Build(report.Input{
		Range: report.Range{From: day("2026-06-01"), To: day("2026-06-02")}, Layout: l,
		Days: []report.Day{d},
	})
	var buf bytes.Buffer
	require.NoError(t, Write(&buf, r))
	assert.Equal(t, "period,from,to,partial,days,complete_days,pv_kwh,consumption_kwh,"+
		"grid_import_kwh,grid_export_kwh,battery_charge_kwh,battery_discharge_kwh,"+
		"battery_grid_charge_est_kwh,consumption_solar_est_kwh,self_sufficiency_pct,"+
		"self_consumption_pct\n"+
		"2026-06-01,2026-06-01,2026-06-01,false,1,1,12.35,8.00,2.00,5.00,1.00,0.90,1.00,6.00,"+
		"75.00,59.50\n"+
		"2026-06-02,2026-06-02,2026-06-02,false,1,0,,,,,,,,,,\n", buf.String())

	grid := report.Build(report.Input{
		Range: report.Range{From: day("2026-06-01"), To: day("2026-06-01")},
		Days: []report.Day{{Date: day("2026-06-01"), Values: map[string]float64{
			report.KeyConsumption: 0, report.KeyImport: 0, report.KeyExport: 0,
		}}},
	})
	buf.Reset()
	require.NoError(t, Write(&buf, grid))
	assert.Equal(t, "period,from,to,partial,days,complete_days,consumption_kwh,"+
		"grid_import_kwh,grid_export_kwh,consumption_solar_est_kwh,self_sufficiency_pct\n"+
		"2026-06-01,2026-06-01,2026-06-01,false,1,1,0.00,0.00,0.00,0.00,\n", buf.String())
	require.Error(t, Write(failing{}, r))
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("disk full") }
