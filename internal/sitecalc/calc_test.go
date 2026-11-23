package sitecalc

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/profiles"
)

func loadProfile(t *testing.T, id string) *profile.Loaded {
	t.Helper()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	p, err := l.Load(id, ".")
	require.NoError(t, err)
	return p
}

// buildSite is the setup R1 + R3 + R4; edit may change the meters before building.
func buildSite(t *testing.T, edit func(map[string]site.MeterSpec)) *site.Site {
	t.Helper()
	meters := map[string]site.MeterSpec{
		"pv":      {Device: "wr1", Source: "pv"},
		"battery": {Device: "wr1", Source: "battery", CapacityWh: 10000},
		"grid":    {Device: "meter", Source: "total", Critical: true},
		"wb-1":    {Device: "goe", Source: "charger"},
		"wb-2":    {Device: "warp", Source: "charger", Parent: "site"},
		"car":     {Device: "warp", Source: "charger", Parent: "wb-2", Category: "other"},
	}
	if edit != nil {
		edit(meters)
	}
	s, err := site.Build(site.Spec{
		Name: "Home",
		Connections: map[string]site.ConnectionSpec{
			"lan": {Address: "tcp://10.0.0.20", Timeout: 2 * time.Second},
		},
		Devices: map[string]site.DeviceSpec{
			"wr1": {
				Connection: "lan", Unit: 1, Profile: loadProfile(t, "solis/s6-hybrid"),
				Interval: 5 * time.Second, Rollover: "23:59",
			},
			"meter": {
				Address: "http://10.0.0.31", Profile: loadProfile(t, "shelly/pro-3em"),
				Interval: 2 * time.Second, Timeout: time.Second,
			},
			"goe": {Address: "http://10.0.0.40", Profile: loadProfile(t, "goe/charger-v2")},
			"warp": {
				Address: "http://10.0.0.41", Profile: loadProfile(t, "tinkerforge/warp3"),
				User: "admin", Password: "pw",
			},
		},
		Meters: meters,
	})
	require.NoError(t, err)
	return s
}

func getter(m map[string]float64) Getter {
	return func(k string) (float64, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func values(m map[string]Result) map[string]float64 {
	out := map[string]float64{}
	for k, r := range m {
		out[k] = r.Value
	}
	return out
}

func TestOutKey(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "site/pv.power", outKey("site/pv"))
}

func TestComputeFullSite(t *testing.T) {
	t.Parallel()
	full := map[string]float64{
		"pv/power": 4000, "grid/power": 500, "battery/power": 1000, "battery/soc": 60,
		"wb-1/power": 1500, "wb-2/power": 2000, "car/power": 1800,
	}
	tests := []struct {
		name string
		in   map[string]float64
		want map[string]float64
	}{
		{
			name: "import and battery discharge",
			in:   full,
			want: map[string]float64{
				"site/pv.power": 4000, "site/grid_in.power": 500, "site/grid_out.power": 0,
				"site/battery_in.power": 0, "site/battery_out.power": 1000, "site/soc": 60,
				"site/consumption.power":        5500,
				"site/loadpoints/wallbox.power": 3500,
				"site/house_rest.power":         2000,
			},
		},
		{
			name: "export and battery charging",
			in: map[string]float64{
				"pv/power": 6000, "grid/power": -2000, "battery/power": -1500,
				"battery/soc": 80, "wb-1/power": 500, "wb-2/power": 0,
			},
			want: map[string]float64{
				"site/pv.power": 6000, "site/grid_in.power": 0, "site/grid_out.power": 2000,
				"site/battery_in.power": 1500, "site/battery_out.power": 0, "site/soc": 80,
				"site/consumption.power":        2500,
				"site/loadpoints/wallbox.power": 500,
				"site/house_rest.power":         2000,
			},
		},
		{
			name: "negative house rest is not clamped",
			in: map[string]float64{
				"pv/power": 0, "grid/power": 1000, "battery/power": 0, "battery/soc": 10,
				"wb-1/power": 1500, "wb-2/power": 500,
			},
			want: map[string]float64{
				"site/pv.power": 0, "site/grid_in.power": 1000, "site/grid_out.power": 0,
				"site/battery_in.power": 0, "site/battery_out.power": 0, "site/soc": 10,
				"site/consumption.power":        1000,
				"site/loadpoints/wallbox.power": 2000,
				"site/house_rest.power":         -1000,
			},
		},
	}
	p := newPlan(buildSite(t, nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := p.compute(getter(tt.in))
			assert.Equal(t, tt.want, values(res))
			assert.False(t, res["site/soc"].Approx)
			assert.Equal(t, "%", res["site/soc"].Unit)
			assert.Equal(t, "W", res["site/pv.power"].Unit)
		})
	}
}

func TestComputeMissingInputs(t *testing.T) {
	t.Parallel()
	p := newPlan(buildSite(t, nil))
	base := func() map[string]float64 {
		return map[string]float64{
			"pv/power": 4000, "grid/power": 500, "battery/power": 0, "battery/soc": 50,
			"wb-1/power": 100, "wb-2/power": 100,
		}
	}
	tests := []struct {
		name    string
		drop    string
		absent  []string
		present []string
	}{
		{
			name:    "no grid",
			drop:    "grid/power",
			absent:  []string{"site/grid_in.power", "site/grid_out.power", "site/consumption.power", "site/house_rest.power"},
			present: []string{"site/pv.power", "site/soc", "site/battery_in.power"},
		},
		{
			name:    "no soc",
			drop:    "battery/soc",
			absent:  []string{"site/soc"},
			present: []string{"site/consumption.power", "site/house_rest.power"},
		},
		{
			name:    "no battery power",
			drop:    "battery/power",
			absent:  []string{"site/battery_in.power", "site/consumption.power"},
			present: []string{"site/pv.power", "site/soc"},
		},
		{
			name:    "no pv",
			drop:    "pv/power",
			absent:  []string{"site/pv.power", "site/consumption.power"},
			present: []string{"site/grid_in.power"},
		},
		{
			name:    "no wallbox",
			drop:    "wb-1/power",
			absent:  []string{"site/loadpoints/wallbox.power", "site/house_rest.power"},
			present: []string{"site/consumption.power"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := base()
			delete(in, tt.drop)
			res := p.compute(getter(in))
			for _, k := range tt.absent {
				assert.NotContains(t, res, k)
			}
			for _, k := range tt.present {
				assert.Contains(t, res, k)
			}
		})
	}
}

func TestComputeTwoBatteries(t *testing.T) {
	t.Parallel()
	in := map[string]float64{
		"pv/power": 0, "grid/power": 0, "battery/power": -500, "bat2/power": 200,
		"battery/soc": 50, "bat2/soc": 100, "wb-1/power": 0, "wb-2/power": 0,
	}
	addBat := func(capacity float64) func(map[string]site.MeterSpec) {
		return func(m map[string]site.MeterSpec) {
			m["bat2"] = site.MeterSpec{Device: "wr1", Source: "battery", CapacityWh: capacity}
		}
	}
	tests := []struct {
		name   string
		cap2   float64
		soc    float64
		approx bool
	}{
		{"weighted", 30000, 87.5, false}, // (50*10000 + 100*30000) / 40000
		{"one without capacity", 0, 75, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := newPlan(buildSite(t, addBat(tt.cap2))).compute(getter(in))
			assert.InDelta(t, tt.soc, res["site/soc"].Value, 1e-9)
			assert.Equal(t, tt.approx, res["site/soc"].Approx)
			assert.InDelta(t, 500, res["site/battery_in.power"].Value, 1e-9)
			assert.InDelta(t, 200, res["site/battery_out.power"].Value, 1e-9)
		})
	}
}

func TestComputeWithoutBattery(t *testing.T) {
	t.Parallel()
	s := buildSite(t, func(m map[string]site.MeterSpec) { delete(m, "battery") })
	res := newPlan(s).compute(getter(map[string]float64{
		"pv/power": 1000, "grid/power": 200, "wb-1/power": 0, "wb-2/power": 0,
	}))
	assert.Equal(t, 1200.0, res["site/consumption.power"].Value)
	for k := range res {
		assert.NotContains(t, k, "battery")
		assert.NotEqual(t, "site/soc", k)
	}
}

// TestComputeBackupStaysInHouseRest: like evcc's home power, the house rest is
// consumption minus loadpoints; the backup output is part of it, not subtracted again
// (wallboxes may hang on the household or the backup side).
func TestComputeBackupStaysInHouseRest(t *testing.T) {
	t.Parallel()
	p := newPlan(buildSite(t, func(m map[string]site.MeterSpec) {
		m["backup"] = site.MeterSpec{Device: "wr1", Source: "backup"}
	}))
	in := map[string]float64{
		"pv/power": 4000, "grid/power": 500, "battery/power": 1000, "battery/soc": 60,
		"wb-1/power": 1500, "wb-2/power": 2000, "car/power": 1800, "backup/power": 300,
	}
	got := values(p.compute(getter(in)))
	assert.InDelta(t, 5500, got["site/consumption.power"], 1e-9)
	assert.InDelta(t, 5500-1500-2000, got["site/house_rest.power"], 1e-9)

	delete(in, "backup/power")
	_, ok := p.compute(getter(in))["site/house_rest.power"]
	assert.True(t, ok, "the backup meter is not an input")
}

// TestComputePVStringsInsideTotal: strings below the PV total are not added again.
func TestComputePVStringsInsideTotal(t *testing.T) {
	t.Parallel()
	p := newPlan(buildSite(t, func(m map[string]site.MeterSpec) {
		m["mppt1"] = site.MeterSpec{Device: "wr1", Source: "pv1", Parent: "pv"}
	}))
	got := values(p.compute(getter(map[string]float64{
		"pv/power": 4000, "mppt1/power": 2500, "grid/power": 0, "battery/power": 0,
		"wb-1/power": 0, "wb-2/power": 0,
	})))
	assert.InDelta(t, 4000, got["site/pv.power"], 1e-9)
}
