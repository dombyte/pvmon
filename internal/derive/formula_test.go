package derive

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/site"
)

func coefs(f Formula) map[string]float64 {
	out := map[string]float64{}
	for _, t := range f.Terms {
		out[t.Series] += t.Coef
	}
	return out
}

func TestCatalogueFull(t *testing.T) {
	t.Parallel()
	cat := Catalogue(buildSite(t, true, true))
	for _, name := range []string{
		"pv/energy_out", "grid/energy_in", "grid/energy_out", "battery/energy_in",
		"battery/energy_out", "wb-2/energy_in", "wb-sc/energy_in",
		site.QuantityPV, site.QuantityGridIn, site.QuantityGridOut, site.QuantityBatteryIn,
		site.QuantityBatteryOut, site.QuantityConsumption, site.QuantityHouseRest,
		site.QuantityLoadpoints + "wallbox", QuantityGridNet, QuantitySelfSufficiency,
		QuantitySelfConsumption,
	} {
		assert.Contains(t, cat, name)
	}
	assert.NotContains(t, cat, site.QuantityLoadpoints+"other", "sub-meter is no loadpoint")
	assert.NotContains(t, cat, site.QuantitySoC, "a gauge")
	assert.Equal(t, map[string]float64{
		"pv/energy_out": 1, "grid/energy_in": 1, "grid/energy_out": -1,
		"battery/energy_out": 1, "battery/energy_in": -1,
	}, coefs(cat[site.QuantityConsumption]))
	assert.Equal(t, map[string]float64{
		"pv/energy_out": 1, "grid/energy_in": 1, "grid/energy_out": -1,
		"battery/energy_out": 1, "battery/energy_in": -1, "wb-2/energy_in": -1,
	}, coefs(cat[site.QuantityHouseRest]), "only direct consumers are subtracted")
	assert.Equal(t, map[string]float64{"wb-2/energy_in": 1},
		coefs(cat[site.QuantityLoadpoints+"wallbox"]))
	assert.Equal(t, map[string]float64{"grid/energy_out": 1, "grid/energy_in": -1},
		coefs(cat[QuantityGridNet]))

	for _, name := range []string{QuantitySelfSufficiency, QuantitySelfConsumption} {
		assert.True(t, cat[name].Ratio(), name)
		assert.NotEmpty(t, cat[name].Den, name)
	}
	assert.False(t, cat[site.QuantityPV].Ratio())
	assert.Equal(t, map[string]float64{"pv/energy_out": 1},
		coefs(Formula{Terms: cat[QuantitySelfConsumption].Den}))
}

func TestCatalogueWithoutBattery(t *testing.T) {
	t.Parallel()
	cat := Catalogue(buildSite(t, false, false))
	assert.NotContains(t, cat, site.QuantityBatteryIn)
	assert.NotContains(t, cat, site.QuantityBatteryOut)
	assert.NotContains(t, cat, "battery/energy_in")
	assert.NotContains(t, cat, "wb-sc/energy_in")
	assert.Equal(t, map[string]float64{
		"pv/energy_out": 1, "grid/energy_in": 1, "grid/energy_out": -1,
	}, coefs(cat[site.QuantityConsumption]))
	keys := make([]string, 0, len(cat))
	for k := range cat {
		keys = append(keys, k)
	}
	assert.True(t, sort.StringsAreSorted(sortedCopy(keys)))
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestFormulaEvaluate(t *testing.T) {
	t.Parallel()
	sum := Formula{Terms: []Term{{"a", 1}, {"b", -1}}}
	ratio := Formula{Terms: []Term{{"a", 1}}, Den: []Term{{"a", 1}, {"b", 1}}}
	full := map[string]Agg{"a": {Wh: 30, Data: true}, "b": {Wh: 10, Data: true}}
	cases := []struct {
		name      string
		f         Formula
		aggs      map[string]Agg
		want      *float64
		complete  bool
		estimated bool
		missing   int
	}{
		{"sum", sum, full, fp(20), true, false, 0},
		{"ratio", ratio, full, fp(75), true, false, 0},
		{"zero denominator", ratio, map[string]Agg{
			"a": {Data: true}, "b": {Data: true},
		}, nil, false, false, 0},
		{"missing term", sum, map[string]Agg{"a": {Wh: 5, Data: true}}, nil, false, false, 0},
		{"no data", sum, nil, nil, false, false, 0},
		{"no terms", Formula{}, nil, nil, false, false, 0},
		{"estimated", sum, map[string]Agg{
			"a": {Wh: 5, Data: true, Estimated: true}, "b": {Wh: 1, Data: true},
		}, fp(4), true, true, 0},
		{"missing days", sum, map[string]Agg{
			"a": {Wh: 5, Data: true, Missing: []string{"d1", "d2"}},
			"b": {Wh: 1, Data: true, Missing: []string{"d2", "d3"}},
		}, fp(4), false, false, 3},
		{"ratio quality from den", ratio, map[string]Agg{
			"a": {Wh: 1, Data: true}, "b": {
				Wh: 1, Data: true, Estimated: true,
				Missing: []string{"d"},
			},
		}, fp(50), false, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.f.Evaluate(tc.aggs)
			if tc.want == nil {
				assert.Nil(t, got.Value)
			} else {
				require.NotNil(t, got.Value)
				assert.InDelta(t, *tc.want, *got.Value, 1e-9)
			}
			assert.Equal(t, tc.complete, got.Complete)
			assert.Equal(t, tc.estimated, got.Estimated)
			assert.Equal(t, tc.missing, got.MissingDays)
		})
	}
}

func fp(v float64) *float64 { return &v }

func TestCatalogueBackupStaysInHouseRest(t *testing.T) {
	t.Parallel()
	s := buildSiteWith(t, false, false, map[string]site.MeterSpec{
		"backup": {Device: "wr1", Source: "backup"},
	})
	got := coefs(Catalogue(s)[site.QuantityHouseRest])
	assert.NotContains(t, got, "backup/energy_in", "the backup output is part of the house")
	assert.InDelta(t, -1, got["wb-2/energy_in"], 0)
}

func TestCataloguePVStringsInsideTotal(t *testing.T) {
	t.Parallel()
	s := buildSiteWith(t, false, false, map[string]site.MeterSpec{
		"mppt1": {Device: "wr1", Source: "pv1", Parent: "pv"},
	})
	cat := Catalogue(s)
	assert.Equal(t, map[string]float64{"pv/energy_out": 1}, coefs(cat[site.QuantityPV]))
	assert.Contains(t, cat, "mppt1/energy_out", "the string keeps its own series")
}
