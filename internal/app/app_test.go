package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/internal/util/clocktest"
	"github.com/dombyte/pvmon/profiles"
)

const devYAML = `
site:
  name: Home
  interval: 7s
connections:
  lan-solis: { address: "tcp://10.0.0.20", timeout: 2s }
devices:
  wr1:   { connection: lan-solis, unit: 1, profile: solis/s6-hybrid, interval: 5s, rollover: "23:59" }
  meter: { address: "http://10.0.0.31", profile: shelly/pro-3em, interval: 2s, timeout: 1s }
  goe:   { address: "http://10.0.0.40", profile: goe/charger-v2 }
  warp:  { address: "http://10.0.0.41", profile: tinkerforge/warp3,
           auth: { user: admin, password: "${env:WARP_PASSWORD}" } }
meters:
  pv:      { device: wr1, source: pv }
  battery: { device: wr1, source: battery, capacity: 10kWh }
  grid:    { device: meter, source: total, critical: true }
  wb-1:    { device: goe, source: charger }
  wb-2:    { device: warp, source: charger, label: Drive }
`

func source(file string) config.Source {
	return config.Source{
		File: file, Environ: []string{},
		Getenv: func(k string) (string, bool) { return "pw", k == "WARP_PASSWORD" },
	}
}

func writeConfig(t *testing.T, dir, yaml string) string {
	t.Helper()
	p := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(yaml), 0o600))
	return p
}

func load(t *testing.T, yaml string) (*app.Loaded, error) {
	t.Helper()
	p := writeConfig(t, t.TempDir(), yaml)
	return app.LoadConfig(source(p), profiles.FS())
}

func TestLoadConfigSuccess(t *testing.T) {
	t.Parallel()
	l, err := load(t, devYAML)
	require.NoError(t, err)
	assert.Equal(t, "Home", l.Site.Name)
	assert.Len(t, l.Site.Devices, 4)
	wr1 := l.Site.Devices["wr1"]
	assert.Equal(t, 5*time.Second, wr1.Interval, "the device overrides the site")
	assert.Equal(t, 7*time.Second, l.Site.Devices["goe"].Interval, "site interval")
	assert.Equal(t, 2*time.Second, wr1.Timeout, "from connection")
	meter := l.Site.Devices["meter"]
	assert.Equal(t, time.Second, meter.Timeout)
	assert.Equal(t, 2*time.Second, l.Site.Devices["goe"].Timeout, "default for inline address")
	warp := l.Site.Devices["warp"]
	assert.Equal(t, "admin", warp.User)
	assert.Equal(t, "pw", warp.Password)
	assert.InDelta(t, 10000, l.Site.Meters["battery"].CapacityWh, 0.001)
	assert.Equal(t, "Drive", l.Site.Meters["wb-2"].Label)
}

func TestLoadConfigProblems(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, old, new, want string
	}{
		{
			"unknown profile", "profile: goe/charger-v2", "profile: goe/nope",
			"devices.goe.profile",
		},
		{
			"two grids", "pv:      { device: wr1, source: pv }",
			"pv: { device: wr1, source: grid, role: grid }", "meters",
		},
		{
			"missing local file", "profile: goe/charger-v2", "profile: ./none.yaml",
			"devices.goe.profile",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Contains(t, devYAML, tc.old)
			_, err := load(t, strings.Replace(devYAML, tc.old, tc.new, 1))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoadConfigLocalProfileRelativeToConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src, err := os.ReadFile("../../profiles/goe/charger-v2.yaml")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "my-goe.yaml"), src, 0o600))
	yaml := strings.Replace(devYAML, "profile: goe/charger-v2", "profile: ./my-goe.yaml", 1)
	p := writeConfig(t, dir, yaml)

	l, err := app.LoadConfig(source(p), profiles.FS())
	require.NoError(t, err)
	got := l.Site.Devices["goe"].Profile
	assert.Equal(t, filepath.Join(dir, "my-goe.yaml"), got.Path)
	assert.True(t, got.Overrides)
}

func TestLoadConfigWithoutFile(t *testing.T) {
	t.Parallel()
	l, err := app.LoadConfig(config.Source{Environ: []string{"WARP_PASSWORD=pw"}}, profiles.FS())
	require.NoError(t, err, "an empty site is the first start before the setup wizard")
	assert.Empty(t, l.Site.Meters)
}

func TestLoadConfigInvalidBuiltinFS(t *testing.T) {
	t.Parallel()
	bad := fstest.MapFS{"x/y.yaml": {Data: []byte("schema: 2\n")}}
	_, err := app.LoadConfig(config.Source{Environ: []string{"WARP_PASSWORD=pw"}}, bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app: profiles")
}

func TestTransportFactories(t *testing.T) {
	t.Parallel()
	l, err := load(t, devYAML)
	require.NoError(t, err)
	clock := clocktest.New(time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	log := zerolog.Nop()

	wr1 := l.Site.Devices["wr1"]
	mbConn := l.Site.Connections[wr1.Connection]
	require.True(t, app.IsModbus(mbConn))
	mb, err := app.CreateModbusConn(mbConn, clock, log)
	require.NoError(t, err)
	require.NotNil(t, mb)

	f, err := app.CreateFetcher(l.Site, wr1, mb, clock, log)
	require.NoError(t, err)
	assert.NotNil(t, f)

	meter := l.Site.Devices["meter"]
	httpConn := l.Site.Connections[meter.Connection]
	assert.False(t, app.IsModbus(httpConn))
	cl, err := app.CreateHTTPClient(httpConn, meter, clock, log)
	require.NoError(t, err)
	assert.NotNil(t, cl)
	f, err = app.CreateFetcher(l.Site, meter, nil, clock, log)
	require.NoError(t, err)
	assert.NotNil(t, f)
}

func TestTransportFactoryErrors(t *testing.T) {
	t.Parallel()
	l, err := load(t, devYAML)
	require.NoError(t, err)
	clock := clocktest.New(time.Now())
	log := zerolog.Nop()

	conn := l.Site.Connections[l.Site.Devices["wr1"].Connection]
	_, err = app.CreateModbusConn(conn, nil, log)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app: modbus")

	// A nil Modbus connection is a missing dependency, not a panic.
	_, err = app.CreateFetcher(l.Site, l.Site.Devices["wr1"], nil, clock, log)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app: fetcher wr1")

	meter := l.Site.Devices["meter"]
	httpConn := l.Site.Connections[meter.Connection]
	_, err = app.CreateHTTPClient(httpConn, meter, nil, log)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app: http meter")
}
