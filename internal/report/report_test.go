package report

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// full is a site with PV, a battery and one wallbox.
var full = Layout{
	PV:         []Meter{{ID: "pv"}},
	Batteries:  []Meter{{ID: "bat"}},
	Loadpoints: []Meter{{ID: "wb", Category: "ev"}},
}

// mkDay builds a complete day of full (Wh) without slots.
func mkDay(date string, pv, cons, imp, exp, chg, dis, wb float64) Day {
	return Day{Date: day(date), Values: map[string]float64{
		KeyPV: pv, KeyConsumption: cons, KeyImport: imp, KeyExport: exp,
		KeyCharge: chg, KeyDischarge: dis, MeterKey(KeyPV, "pv"): pv,
		MeterKey(KeyCharge, "bat"): chg, MeterKey(KeyDischarge, "bat"): dis,
		MeterKey(KeyLoadpoint, "wb"): wb,
	}}
}

func ptr(v float64) *float64 { return &v }

func TestSummary(t *testing.T) {
	t.Parallel()
	d1 := mkDay("2026-06-01", 20000, 10000, 2000, 9000, 4000, 3000, 1000)
	d2 := mkDay("2026-06-02", 10000, 10000, 4000, 2000, 3000, 2000, 0)
	d2.Slots = &DaySlots{GridCharge: 500, Solar: map[string]SolarParts{}}
	incomplete := Day{Date: day("2026-06-03"), Values: map[string]float64{KeyPV: 1}}
	r := Build(Input{
		Range: rng("2026-06-01", "2026-06-04"), Layout: full,
		Days: []Day{d2, incomplete, d1, mkDay("2026-07-01", 1, 1, 1, 1, 1, 1, 1)},
	})
	s := r.Summary
	assert.Equal(t, 4, s.Days)
	assert.Equal(t, 2, s.CompleteDays)
	assert.False(t, s.Complete())
	assert.Equal(t, Energy{30000, 20000, 6000, 11000, 7000, 5000}, s.Total)
	assert.Equal(t, Energy{15000, 10000, 3000, 5500, 3500, 2500}, *s.PerDay)
	assert.InDelta(t, 0.7, *s.SelfSufficiency, 1e-9)
	assert.InDelta(t, 19.0/30, *s.SelfConsumption, 1e-9)
	// d1 falls back to min(charge, import) = 2000, d2 has 500 from its slots.
	assert.InDelta(t, 2500, s.GridCharge, 1e-9)
	assert.Equal(t, 1, s.FallbackDays)
	assert.InDelta(t, 6000, s.Sources.Grid, 1e-9)
	assert.InDelta(t, 2500, s.Sources.Battery, 1e-9)
	assert.InDelta(t, 11500, s.Sources.Solar, 1e-9)
	assert.InDelta(t, 4500, s.PVSplit.Battery, 1e-9)
	assert.InDelta(t, 14500, s.PVSplit.Solar, 1e-9)
	assert.Nil(t, s.BatteryRatio, "below 28 days")
	assert.Equal(t, []time.Time{day("2026-06-03"), day("2026-06-04")}, r.MissingDays)
	require.Len(t, r.Buckets, 4)
	assert.Equal(t, "2026-06-01", r.Buckets[0].Key)
	assert.Equal(t, 0, r.Buckets[2].Summary.CompleteDays)
	assert.Nil(t, r.Batteries, "one battery has no own section")
	assert.Nil(t, r.PVSources)
	assert.Equal(t, day("2026-06-01"), r.Patterns.BestPV.Date)
	ids := make([]string, len(r.Figures))
	for i, f := range r.Figures {
		ids[i] = f.ID
	}
	assert.Equal(t, []string{
		"pv", "consumption", "self_sufficiency", "self_consumption",
		"grid_import", "grid_export", "battery_charge", "battery_discharge",
	}, ids)
}

func TestBalanceAndSections(t *testing.T) {
	t.Parallel()
	l := Layout{
		PV:        []Meter{{ID: "a", KWp: 5}, {ID: "b"}},
		Batteries: []Meter{{ID: "x"}, {ID: "y"}},
	}
	var days []Day
	start := day("2026-03-01")
	for i := range MinBalanceDays {
		d := Day{Date: start.AddDate(0, 0, i), Values: map[string]float64{
			KeyPV: 4000, KeyConsumption: 3000, KeyImport: 500, KeyExport: 1000,
			KeyCharge: 1000, KeyDischarge: 400, MeterKey(KeyPV, "a"): 3000,
			MeterKey(KeyPV, "b"): 1000, MeterKey(KeyCharge, "x"): 600,
			MeterKey(KeyCharge, "y"): 400, MeterKey(KeyDischarge, "x"): 300,
			MeterKey(KeyDischarge, "y"): 100,
		}}
		days = append(days, d)
	}
	r := Build(Input{
		Range: Range{start, start.AddDate(0, 0, MinBalanceDays-1)}, Layout: l,
		Days: days, Granularity: ByMonth,
	})
	s := r.Summary
	require.NotNil(t, s.BatteryRatio)
	assert.InDelta(t, 0.4, *s.BatteryRatio, 1e-9)
	// in 4900 − out 5000 per day
	assert.InDelta(t, -100*MinBalanceDays, *s.BalanceDiff, 1e-6)
	assert.InDelta(t, -100.0/4900, *s.BalanceDiffShare, 1e-9)
	require.NotNil(t, r.PVSources)
	assert.InDelta(t, 0.75, *r.PVSources.Meters[0].Share, 1e-9)
	assert.InDelta(t, 3.0*MinBalanceDays/5, *r.PVSources.Meters[0].SpecificYield, 1e-9)
	assert.Nil(t, r.PVSources.Meters[1].SpecificYield)
	require.NotNil(t, r.Batteries)
	assert.InDelta(t, 0.5, *r.Batteries.Meters[0].Ratio, 1e-9)
	assert.InDelta(t, 0.25, *r.Batteries.Meters[1].Ratio, 1e-9)
	require.Len(t, r.Buckets, 1)
	assert.Equal(t, "2026-03", r.Buckets[0].Key)
	assert.True(t, r.Buckets[0].Partial)
	assert.Nil(t, r.Loadpoints)
}

func TestLoadpointsAndProfile(t *testing.T) {
	t.Parallel()
	at := func(h, m int) time.Time { return time.Date(2026, 6, 1, h, m, 0, 0, time.UTC) }
	slots := []Slot{
		{At: at(12, 0), Values: map[string]float64{
			KeyConsumption: 1000, KeyImport: 0,
			MeterKey(KeyLoadpoint, "wb"): 800, KeyCharge: 100,
		}},
		{At: at(20, 15), Values: map[string]float64{
			KeyConsumption: 500, KeyImport: 250,
			MeterKey(KeyLoadpoint, "wb"): 200, KeyCharge: 300,
		}},
	}
	est := EstimateDay(slots, []string{"wb"})
	require.NotNil(t, est)
	// The 250 Wh import of the evening charge the battery: none of it reaches the wallbox.
	assert.InDelta(t, 1000, est.Solar["wb"].NotImported, 1e-9)
	assert.InDelta(t, 250, est.GridCharge, 1e-9)
	assert.InDelta(t, 4000, est.Peak, 1e-9)
	assert.Equal(t, at(12, 0), est.PeakAt)
	assert.Nil(t, EstimateDay(append(slots, Slot{Gap: true}), nil))
	assert.Nil(t, EstimateDay(nil, nil))
	assert.Zero(t, solarShare(0, 10))

	d1 := mkDay("2026-06-01", 9000, 1500, 250, 7000, 400, 0, 1000)
	d1.Slots = est
	d2 := mkDay("2026-06-02", 9000, 2000, 500, 7000, 0, 0, 1000) // fallback: share 0.75
	l := full
	l.Loadpoints = append(l.Loadpoints, Meter{ID: "hp", Category: "heat"})
	d1.Values[MeterKey(KeyLoadpoint, "hp")] = 0
	d2.Values[MeterKey(KeyLoadpoint, "hp")] = 400
	r := Build(Input{Range: rng("2026-06-01", "2026-06-02"), Layout: l, Days: []Day{d1, d2}})
	lp := r.Loadpoints
	require.NotNil(t, lp)
	assert.InDelta(t, 2000, lp.Meters[0].In, 1e-9)
	assert.InDelta(t, 1750, lp.Meters[0].Solar, 1e-9)
	assert.InDelta(t, 0.875, *lp.Meters[0].SolarShare, 1e-9)
	require.Len(t, lp.Categories, 2)
	assert.Equal(t, "heat", lp.Categories[1].Category)
	assert.InDelta(t, 300, lp.Categories[1].Solar, 1e-9)
	house := d1.Values[KeyConsumption] + d2.Values[KeyConsumption] - lp.Meters[0].In - lp.Meters[1].In
	assert.InDelta(t, house, *lp.House, 1e-9)
	// Consumption solar 1500 (slots) + 1500 (daily balance) − loadpoints 2050.
	assert.InDelta(t, 950, lp.HouseSolar, 1e-9)
	assert.InDelta(t, 950/house, *lp.HouseSolarShare, 1e-9)
	d2.Values[MeterKey(KeyLoadpoint, "hp")] = 5000 // the meters disagree: House < 0
	neg := Build(Input{Range: rng("2026-06-01", "2026-06-02"), Layout: l, Days: []Day{d1, d2}})
	assert.Zero(t, neg.Loadpoints.HouseSolar)
	assert.Nil(t, neg.Loadpoints.HouseSolarShare)
	assert.InDelta(t, 4000, *r.Profile.Mean[48], 1e-9)
	assert.InDelta(t, 2000, *r.Profile.Mean[81], 1e-9)
	assert.Nil(t, r.Profile.Mean[0])
	assert.InDelta(t, 4000, *r.Profile.Peak, 1e-9)
}

func TestSolarWithGridCharge(t *testing.T) {
	t.Parallel()
	slot := func(cons, imp, chg, dis float64) Slot {
		return Slot{Values: map[string]float64{
			KeyConsumption: cons, KeyImport: imp, KeyCharge: chg, KeyDischarge: dis,
			MeterKey(KeyLoadpoint, "wb"): cons,
		}}
	}
	solar := func(slots ...Slot) float64 {
		d := mkDay("2026-06-01", 0, 4000, 0, 0, 0, 0, 4000)
		d.Slots = EstimateDay(slots, []string{"wb"})
		r := Build(Input{Range: rng("2026-06-01", "2026-06-01"), Layout: full, Days: []Day{d}})
		return r.Loadpoints.Meters[0].Solar
	}
	// Night: 1000 from the grid while 5000 charge from it. Later 8000 from the battery,
	// 5/8 of which were bought: 3000 of them are solar.
	assert.InDelta(t, 3000, solar(slot(1000, 6000, 5000, 0), slot(8000, 0, 0, 8000)), 1e-9)
	// More bought than delivered: the whole battery counts as grid.
	assert.InDelta(t, 0, solar(slot(0, 5000, 5000, 0), slot(2000, 0, 0, 2000)), 1e-9)
	// No grid charge: the battery counts as solar.
	assert.InDelta(t, 1500, solar(slot(2000, 500, 0, 1000)), 1e-9)
}

func TestComparison(t *testing.T) {
	t.Parallel()
	cur := []Day{
		mkDay("2026-03-01", 1000, 1000, 500, 0, 0, 0, 0),
		mkDay("2026-03-02", 2000, 1000, 0, 500, 0, 0, 0),
	}
	base := []Day{
		mkDay("2025-03-01", 500, 1000, 600, 0, 0, 0, 0),
		mkDay("2025-03-02", 500, 1000, 600, 0, 0, 0, 0),
	}
	in := Input{
		Range: rng("2026-03-01", "2026-03-02"), Layout: full, Days: cur,
		Compare: &CompareInput{
			Mode: CompareLastYear, Range: rng("2025-03-01", "2025-03-02"),
			Days: base,
		},
	}
	r := Build(in)
	require.NotNil(t, r.Comparison)
	assert.False(t, r.Comparison.Matched)
	pv := r.Figures[0]
	assert.InDelta(t, 2000, *pv.Change, 1e-9)
	assert.InDelta(t, 2, *pv.ChangePct, 1e-9)
	ss := r.Figures[2]
	assert.Equal(t, KindRatio, ss.Kind)
	assert.InDelta(t, 0.75-0.4, *ss.Change, 1e-9)
	assert.Nil(t, ss.ChangePct)

	in.Compare.Days = base[1:] // 2025-03-01 missing: compare the matched day only
	r = Build(in)
	assert.True(t, r.Comparison.Matched)
	assert.Equal(t, 1, r.Comparison.MatchedDays)
	assert.InDelta(t, 2000, *r.Figures[0].Compared, 1e-9)
	assert.InDelta(t, 3000, *r.Figures[0].Value, 1e-9)

	in.Compare.Days = nil
	r = Build(in)
	assert.Nil(t, r.Figures[0].Base)
	assert.Nil(t, r.Figures[0].Change)
}

func TestGridOnlySite(t *testing.T) {
	t.Parallel()
	d := Day{Date: day("2026-06-01"), Values: map[string]float64{
		KeyConsumption: 4000, KeyImport: 4000, KeyExport: 0,
	}}
	r := Build(Input{Range: rng("2026-06-01", "2026-06-01"), Days: []Day{d}})
	assert.Equal(t, 1, r.Summary.CompleteDays)
	assert.InDelta(t, 0, *r.Summary.SelfSufficiency, 1e-9)
	assert.Nil(t, r.Summary.SelfConsumption)
	assert.Nil(t, r.Patterns.BestPV)
	assert.NotNil(t, r.Patterns.MaxImport)
	assert.Len(t, r.Figures, 4)
	assert.InDelta(t, 1, *r.Summary.Sources.GridShare, 1e-9)
	assert.Equal(t, ptr(0), r.Summary.Sources.BatteryShare)
}
