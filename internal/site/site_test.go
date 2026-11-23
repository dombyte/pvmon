package site

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
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

// devSpec is the combined setup R1 + R3 + R4.
func devSpec(t *testing.T) Spec {
	t.Helper()
	return Spec{
		Name: "Home",
		Connections: map[string]ConnectionSpec{
			"lan-solis": {Address: "tcp://10.0.0.20", Timeout: 2 * time.Second},
		},
		Devices: map[string]DeviceSpec{
			"wr1": {
				Connection: "lan-solis", Unit: 1, Profile: loadProfile(t, "solis/s6-hybrid"),
				Interval: 5 * time.Second, Rollover: "23:59", History: []string{"voltage"},
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
		Meters: map[string]MeterSpec{
			"pv":      {Device: "wr1", Source: "pv"},
			"battery": {Device: "wr1", Source: "battery", CapacityWh: 10000},
			"inv":     {Device: "wr1", Source: "grid", Role: "inverter"},
			"backup":  {Device: "wr1", Source: "backup"},
			"grid":    {Device: "meter", Source: "total", Critical: true},
			"wb-1":    {Device: "goe", Source: "charger"},
			"wb-2":    {Device: "warp", Source: "charger", Parent: "site", Label: "Drive"},
			"car":     {Device: "warp", Source: "charger", Parent: "wb-2", Category: "other"},
		},
	}
}

func TestBuildCombinedSite(t *testing.T) {
	t.Parallel()
	s, err := Build(devSpec(t))
	require.NoError(t, err)
	assert.Len(t, s.Connections, 4)
	conn := s.Connections["tcp://10.0.0.20:502"]
	require.NotNil(t, conn)
	assert.Equal(t, []string{"wr1"}, conn.Devices)
	wr1 := s.Devices["wr1"]
	assert.Equal(t, 2*time.Second, wr1.Timeout, "from connection")
	assert.Equal(t, profile.ReadRange, wr1.Read)
	assert.Equal(t, "app", wr1.Netting, "profile default")
	assert.Equal(t, "meter", s.Devices["goe"].Netting)
	assert.Equal(t, []string{"backup", "battery", "inv", "pv"}, wr1.Meters)
	assert.Equal(t, "wallbox", s.Meters["wb-1"].Category, "from block")
	assert.Equal(t, ParentSite, s.Meters["wb-1"].Parent)
	assert.Equal(t, "wb-1", s.Meters["wb-1"].Label)

	caps := s.Capabilities()
	assert.Equal(t, 1, caps.PV)
	assert.Equal(t, 1, caps.Battery)
	assert.True(t, caps.Grid)
	assert.True(t, caps.Backup)
	assert.Equal(t, 2, caps.Loadpoints["wallbox"])
	assert.Equal(t, 0, caps.Loadpoints["other"], "car is below wb-2")
	assert.Equal(t, []string{
		QuantityPV, QuantityGridIn, QuantityGridOut, QuantityBatteryIn, QuantityBatteryOut,
		QuantitySoC, QuantityConsumption, QuantityHouseRest, QuantityLoadpoints + "wallbox",
	}, s.Quantities())

	keys := map[string]SeriesInfo{}
	for _, si := range s.Series() {
		keys[si.Key()] = si
	}
	assert.Contains(t, keys, "pv/energy_out")
	assert.Contains(t, keys, "battery/voltage", "history list")
	assert.NotContains(t, keys, "pv/energy_total", "history: false")
	assert.Equal(t, KindEnergy, keys["grid/energy_in"].Kind)
	assert.Equal(t, "Wh", keys["wb-2/energy_in"].Unit)
	assert.Equal(t, KindGauge, keys["battery/soc"].Kind)
	assert.NotContains(t, keys, "grid/grid_import_raw", "shelly nets itself")
}

// TestPVStringsBelowTotal: strings below the PV total are part of it, not extra sources.
func TestPVStringsBelowTotal(t *testing.T) {
	t.Parallel()
	spec := devSpec(t)
	spec.Meters["mppt1"] = MeterSpec{Device: "wr1", Source: "pv1", Parent: "pv"}
	spec.Meters["mppt2"] = MeterSpec{Device: "wr1", Source: "pv2", Parent: "pv"}
	s, err := Build(spec)
	require.NoError(t, err)
	assert.Equal(t, 1, s.Capabilities().PV)
	assert.Equal(t, ParentSite, s.Meters["pv"].Parent)
	assert.Equal(t, "pv", s.Meters["mppt1"].Parent)

	spec.Meters["mppt3"] = MeterSpec{Device: "wr1", Source: "pv3", Parent: "mppt1"}
	_, err = Build(spec)
	require.ErrorContains(t, err, `meters.mppt3.parent: parent "mppt1" is not a top-level pv meter`)
}

func TestSeriesNettingAndOrigins(t *testing.T) {
	t.Parallel()
	spec := devSpec(t)
	spec.Meters["grid"] = MeterSpec{Device: "wr1", Source: "grid"}
	delete(spec.Meters, "inv")
	spec.Meters["pv1"] = MeterSpec{Device: "wr1", Source: "pv1"}
	s, err := Build(spec)
	require.NoError(t, err)
	keys := map[string]SeriesInfo{}
	for _, si := range s.Series() {
		keys[si.Key()] = si
	}
	assert.True(t, keys["grid/grid_import_raw"].Hidden)
	assert.Contains(t, keys, "grid/grid_cancelled")
	assert.Equal(t, OriginShare, keys["pv1/energy_out"].Origin)
	assert.Equal(t, OriginMeasured, keys["pv/energy_out"].Origin)
	assert.Equal(t, "Wh", keys["pv1/energy_out"].Unit)
	assert.Equal(t, "W", unit(profile.Channel{Unit: "kW"}))
	assert.Equal(t, OriginIntegrated, origin(profile.Channel{Unit: "Wh"}))
}

// An empty site is the first start before the setup wizard; a device without meters is
// allowed too (the wizard adds the meters later).
func TestBuildEmptySite(t *testing.T) {
	t.Parallel()
	s, err := Build(Spec{Name: "Home"})
	require.NoError(t, err)
	assert.Empty(t, s.Meters)
}

func TestBuildProblems(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(s *Spec)
		want   string
	}{
		{"nogrid", func(s *Spec) { delete(s.Meters, "grid") }, "found none"},
		{"twogrids", func(s *Spec) {
			s.Meters["grid2"] = MeterSpec{Device: "wr1", Source: "grid", Role: "grid"}
		}, "found [grid grid2]"},
		{"cycle", func(s *Spec) {
			s.Meters["wb-2"] = MeterSpec{Device: "warp", Source: "charger", Parent: "car"}
		}, "meters.car.parent: parent chain forms a cycle"},
		{"unknownparent", func(s *Spec) {
			s.Meters["car"] = MeterSpec{Device: "warp", Source: "charger", Parent: "nope"}
		}, `unknown meter "nope"`},
		{"parentnotconsumer", func(s *Spec) {
			s.Meters["car"] = MeterSpec{Device: "warp", Source: "charger", Parent: "pv"}
		}, `parent "pv" is not a consumer`},
		{"role", func(s *Spec) {
			s.Meters["pv"] = MeterSpec{Device: "wr1", Source: "pv", Role: "battery"}
		}, `does not allow role "battery"`},
		{
			"source", func(s *Spec) { s.Meters["pv"] = MeterSpec{Device: "wr1", Source: "pv9"} },
			`has no block "pv9"`,
		},
		{
			"device", func(s *Spec) { s.Meters["pv"] = MeterSpec{Device: "x", Source: "pv"} },
			`meters.pv.device: unknown device "x"`,
		},
		{"category", func(s *Spec) {
			s.Meters["wb-1"] = MeterSpec{Device: "goe", Source: "charger", Category: "car"}
		}, `unknown category "car"`},
		{"categorynonconsumer", func(s *Spec) {
			s.Meters["pv"] = MeterSpec{Device: "wr1", Source: "pv", Category: "other"}
		}, "only consumers have a category"},
		{"parentnonconsumer", func(s *Spec) {
			s.Meters["pv"] = MeterSpec{Device: "wr1", Source: "pv", Parent: "wb-1"}
		}, "is not a top-level pv meter"},
		{"parentbattery", func(s *Spec) {
			s.Meters["battery"] = MeterSpec{Device: "wr1", Source: "battery", Parent: "pv"}
		}, "only consumers and pv meters can have a meter as parent"},
		{"address", func(s *Spec) {
			s.Connections["lan-solis"] = ConnectionSpec{Address: "udp://x"}
		}, "scheme must be"},
		{"transport", func(s *Spec) {
			d := s.Devices["goe"]
			d.Address = "tcp://10.0.0.40"
			s.Devices["goe"] = d
		}, "needs a http connection"},
		{"unit", func(s *Spec) {
			d := s.Devices["wr1"]
			d.Unit = 300
			s.Devices["wr1"] = d
		}, "devices.wr1.unit"},
		{"rollover", func(s *Spec) {
			d := s.Devices["goe"]
			d.Rollover = "23:59"
			s.Devices["goe"] = d
		}, "has no daily_reset"},
		{"noprofile", func(s *Spec) {
			d := s.Devices["goe"]
			d.Profile = nil
			s.Devices["goe"] = d
		}, "devices.goe.profile: no profile"},
		{"conn", func(s *Spec) {
			d := s.Devices["wr1"]
			d.Connection = "nope"
			s.Devices["wr1"] = d
		}, `unknown connection "nope"`},
		{"serial", func(s *Spec) {
			s.Connections["a"] = ConnectionSpec{
				Address: "rtu:///dev/ttyUSB0",
				Serial:  Serial{Speed: 9600},
			}
			s.Connections["b"] = ConnectionSpec{
				Address: "rtu:///dev/ttyUSB0",
				Serial:  Serial{Speed: 19200},
			}
		}, "serial parameters conflict"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := devSpec(t)
			tc.mutate(&spec)
			_, err := Build(spec)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestReadMode(t *testing.T) {
	t.Parallel()
	direct := *loadProfile(t, "solis/s6-hybrid")
	p := *direct.Profile
	r := *p.Read
	r.Mode = profile.ReadDirect
	p.Read = &r
	direct.Profile = &p
	spec := devSpec(t)
	d := spec.Devices["wr1"]
	d.Read = profile.ReadDirect
	spec.Devices["wr1"] = d
	s, err := Build(spec)
	require.NoError(t, err)
	assert.Equal(t, profile.ReadDirect, s.Devices["wr1"].Read)

	d.Profile = &direct
	d.Read = profile.ReadRange
	spec.Devices["wr1"] = d
	_, err = Build(spec)
	require.ErrorContains(t, err, "range is not allowed")
	d.Read = ""
	d.Netting = "meter"
	spec.Devices["wr1"] = d
	s, err = Build(spec)
	require.NoError(t, err)
	assert.Equal(t, profile.ReadDirect, s.Devices["wr1"].Read)
	assert.Equal(t, "meter", s.Devices["wr1"].Netting)
	assert.Empty(t, s.Devices["goe"].Read)
}

func TestIntegratedSeries(t *testing.T) {
	t.Parallel()
	blk := profile.Block{Role: "pv", Channels: map[string]profile.Channel{
		"power": {Unit: "W"},
	}}
	got := integratedSeries(&Meter{ID: "micro", Role: "pv", Block: blk})
	require.Len(t, got, 1)
	assert.Equal(t, "micro/energy_out", got[0].Key())
	assert.Equal(t, OriginIntegrated, got[0].Origin)
	assert.Len(t, integratedSeries(&Meter{ID: "g", Role: "grid", Block: blk}), 2)
	assert.Empty(t, integratedSeries(&Meter{ID: "x", Role: "phase", Block: blk}))
	assert.Empty(t, integratedSeries(&Meter{Block: profile.Block{}}))
	for _, role := range []string{"battery", "inverter", "consumer", "backup"} {
		pos, _ := IntegratedChannels(role)
		assert.NotEmpty(t, pos, role)
	}
}

func TestParseAddress(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"tcp://10.0.0.5":          "tcp://10.0.0.5:502",
		"tcp://host:1502":         "tcp://host:1502",
		"rtu:///dev/ttyUSB0":      "rtu:///dev/ttyUSB0",
		"http://10.0.0.30":        "http://10.0.0.30",
		"https://10.0.0.30:8443/": "https://10.0.0.30:8443",
	} {
		got, _, err := ParseAddress(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got)
	}
	for _, in := range []string{"tcp://", "tcp://h/x", "rtu://host", "http://", "x", "%zz"} {
		_, _, err := ParseAddress(in)
		assert.Error(t, err, in)
	}
	e := &Error{Problems: []Problem{{Path: "a", Message: "b"}}}
	assert.True(t, strings.HasPrefix(e.Error(), "site: a: b"))
}
