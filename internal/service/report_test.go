package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/derive"
	"github.com/dombyte/pvmon/internal/report"
	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/service/mocks"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

// dayWh is the daily energy the fake deriver returns per name.
var dayWh = map[string]float64{
	"site/pv": 20000, "site/consumption": 9600, "site/grid_in": 2400, "site/grid_out": 9000,
	"site/battery_in": 4000, "site/battery_out": 3000, "pv/energy_out": 20000,
	"battery/energy_in": 4000, "battery/energy_out": 3000, "grid/energy_in": 2400,
	"grid/energy_out": 9000, "wb-2/energy_in": 2000, "backup/energy_in": 500,
}

// slotWh is the energy of every bucket (96 per day).
var slotWh = map[string]float64{
	"site/consumption": 100, "site/grid_in": 25, "site/battery_in": 10, "wb-2/energy_in": 20,
}

func fakeEnergy(_ context.Context, names []string, level derive.Level, from, to time.Time,
) ([]derive.Result, error) {
	periods, err := derive.Periods(level, from, to, time.UTC)
	if err != nil {
		return nil, err
	}
	out := make([]derive.Result, len(names))
	for i, n := range names {
		out[i].Name = n
		for _, p := range periods {
			out[i].Values = append(out[i].Values, fakeValue(n, level, p.Key))
		}
	}
	return out, nil
}

func fakeValue(name string, level derive.Level, key string) derive.PeriodValue {
	values := dayWh
	if level == derive.LevelBucket {
		values = slotWh
	}
	v, ok := values[name]
	pv := derive.PeriodValue{Period: key, Value: derive.Value{Value: &v, Complete: ok}}
	switch name + "@" + key {
	case "battery/energy_in@2026-06-02":
		pv.Complete, pv.Estimated = false, true
	case "site/grid_in@2026-06-03T12:00:00Z":
		pv.Complete = false // a gap: that day falls back to the daily balance
	}
	return pv
}

func newReports(t *testing.T, energy func(context.Context, []string, derive.Level, time.Time,
	time.Time) ([]derive.Result, error),
) *service.Reports {
	t.Helper()
	d := mocks.NewMockEnergyDeriver(t)
	d.EXPECT().Names().Return(append(keys(dayWh), "site/self_sufficiency"))
	d.EXPECT().Energy(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).RunAndReturn(energy).Maybe()
	clock := clocktest.New(time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC))
	svc, err := service.NewReports(devSite(t), d, clock, time.UTC)
	require.NoError(t, err)
	return svc
}

func keys(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestReports(t *testing.T) {
	t.Parallel()
	svc := newReports(t, fakeEnergy)
	r, err := svc.Build(t.Context(), service.ReportQuery{
		From: "2026-06-01", To: "2026-06-05", Compare: "previous",
	})
	require.NoError(t, err)
	assert.True(t, r.TodayExcluded)
	assert.Equal(t, 4, r.Summary.Days)
	assert.Equal(t, 4, r.Summary.CompleteDays)
	assert.Equal(t, 1, r.Summary.FallbackDays)
	// 3 days of 96 × min(10, 25) from the slots, 1 day min(4000, 2400) from the balance.
	assert.InDelta(t, 3*960+2400, r.Summary.GridCharge, 1e-9)
	require.NotNil(t, r.Loadpoints)
	assert.Equal(t, "wb-2", r.Loadpoints.Meters[0].ID)
	assert.InDelta(t, 3*96*20*0.75+2000*0.75, r.Loadpoints.Meters[0].Solar, 1e-9)
	assert.InDelta(t, 400, *r.Profile.Mean[0], 1e-9)
	require.NotNil(t, r.Comparison)
	assert.Equal(t, "2026-05-01", r.Comparison.Range.From.Format(time.DateOnly))
	assert.Nil(t, r.Batteries, "a single battery")
	assert.Equal(t, []report.MeterQuality{
		{ID: "backup", Label: "backup"},
		{ID: "battery", Label: "battery", MissingDays: 1, EstimatedDays: 1},
		{ID: "grid", Label: "grid"},
		{ID: "pv", Label: "pv"},
		{ID: "wb-2", Label: "Drive"},
	}, r.Quality)

	m := service.NewReportModel(r)
	b, err := json.Marshal(m)
	require.NoError(t, err)
	var back map[string]any
	require.NoError(t, json.Unmarshal(b, &back))
	sum := back["summary"].(map[string]any) //nolint:forcetypeassert // shape under test
	assert.InDelta(t, 75.0, sum["self_sufficiency_pct"], 1e-9)
	assert.InDelta(t, 38.4, sum["total"].(map[string]any)["consumption_kwh"], 1e-9)                              //nolint:forcetypeassert // shape under test
	assert.Equal(t, "previous", back["comparison"].(map[string]any)["mode"])                                     //nolint:forcetypeassert // shape under test
	assert.Len(t, back["profile"].(map[string]any)["mean_w"], report.SlotsPerDay)                                //nolint:forcetypeassert // shape under test
	assert.Equal(t, "Drive", back["loadpoints"].(map[string]any)["meters"].([]any)[0].(map[string]any)["label"]) //nolint:forcetypeassert // shape under test
}

func TestReportErrors(t *testing.T) {
	t.Parallel()
	svc := newReports(t, fakeEnergy)
	for _, q := range []service.ReportQuery{
		{From: "x", To: "2026-06-01"},
		{From: "2026-06-05", To: "2026-06-05"},
		{From: "2026-06-01", To: "2026-06-02", Granularity: "week"},
		{From: "2026-06-01", To: "2026-06-02", Compare: "always"},
	} {
		_, err := svc.Build(t.Context(), q)
		require.ErrorIs(t, err, service.ErrInvalidReport, "%+v", q)
	}
	boom := errors.New("boom")
	for _, failLevel := range []derive.Level{derive.LevelDay, derive.LevelBucket} {
		svc := newReports(t, func(ctx context.Context, n []string, l derive.Level, f, to time.Time,
		) ([]derive.Result, error) {
			if l == failLevel {
				return nil, boom
			}
			return fakeEnergy(ctx, n, l, f, to)
		})
		_, err := svc.Build(t.Context(), service.ReportQuery{From: "2026-06-01", To: "2026-06-01"})
		require.ErrorIs(t, err, boom)
	}
	svc = newReports(t, func(_ context.Context, n []string, l derive.Level, _, _ time.Time,
	) ([]derive.Result, error) {
		out := make([]derive.Result, len(n))
		for i := range n {
			out[i] = derive.Result{Name: n[i], Values: []derive.PeriodValue{{Period: "bad"}}}
		}
		return out, nil
	})
	_, err := svc.Build(t.Context(), service.ReportQuery{From: "2026-06-01", To: "2026-06-01"})
	require.ErrorContains(t, err, "report slot")
	_, err = service.NewReports(nil, nil, nil, nil)
	require.Error(t, err)
}
