package derive

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/profiles"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	return loc
}

func loadProfile(t *testing.T, id string) *profile.Loaded {
	t.Helper()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	p, err := l.Load(id, ".")
	require.NoError(t, err)
	return p
}

// buildSite is a reduced R1 + R3 + R4 setup: solis (pv, optional battery), shelly grid,
// a warp wallbox with an optional sub-meter car below it.
func buildSite(t *testing.T, battery, car bool) *site.Site {
	t.Helper()
	return buildSiteWith(t, battery, car, nil)
}

// buildSiteWith is buildSite with extra meters.
func buildSiteWith(t *testing.T, battery, car bool, extra map[string]site.MeterSpec) *site.Site {
	t.Helper()
	meters := map[string]site.MeterSpec{
		"pv":    {Device: "wr1", Source: "pv"},
		"grid":  {Device: "meter", Source: "total", Critical: true},
		"wb-2":  {Device: "warp", Source: "charger", Parent: "site"},
		"wb-sc": {Device: "warp", Source: "charger", Parent: "wb-2", Category: "other"},
	}
	if battery {
		meters["battery"] = site.MeterSpec{Device: "wr1", Source: "battery", CapacityWh: 10000}
	}
	if !car {
		delete(meters, "wb-sc")
	}
	for id, m := range extra {
		meters[id] = m
	}
	s, err := site.Build(site.Spec{
		Name: "Home",
		Connections: map[string]site.ConnectionSpec{
			"c": {Address: "tcp://10.0.0.20", Timeout: 2 * time.Second},
		},
		Devices: map[string]site.DeviceSpec{
			"wr1": {
				Connection: "c", Unit: 1, Profile: loadProfile(t, "solis/s6-hybrid"),
				Interval: 5 * time.Second, Rollover: "23:59",
			},
			"meter": {
				Address: "http://10.0.0.31", Profile: loadProfile(t, "shelly/pro-3em"),
				Interval: 2 * time.Second, Timeout: time.Second,
			},
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
