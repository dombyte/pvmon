package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const siteYAML = `
site:
  name: Home
connections:
  lan-solis: { address: "tcp://10.0.0.20:502", timeout: 2s }
devices:
  wr1:   { connection: lan-solis, unit: 1, profile: solis/s6-hybrid, interval: 5s, rollover: "23:59" }
  warp:  { address: "http://10.0.0.41", profile: tinkerforge/warp3, interval: 10s,
           auth: { user: admin, password: "${env:WARP_PASSWORD}" } }
meters:
  battery: { device: wr1, source: battery, role: battery, capacity: 10kWh }
  grid:    { device: wr1, source: grid, role: grid, critical: true }
  wb:      { device: warp, source: charger, role: consumer, category: wallbox, parent: site }
`

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func getenv(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func problems(t *testing.T, err error) []string {
	t.Helper()
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	out := make([]string, len(ve.Problems))
	for i, p := range ve.Problems {
		out[i] = p.String()
	}
	return out
}

func TestEveryKeyHasDefault(t *testing.T) {
	t.Parallel()
	defs := Defaults()
	objDefs := ObjectDefaults()
	for _, key := range KnownKeys() {
		if _, ok := defs[key]; ok {
			continue
		}
		parts := strings.SplitN(key, ".", 3)
		require.Len(t, parts, 3, "key %s has no default", key)
		_, ok := objDefs[parts[0]][parts[2]]
		assert.True(t, ok, "key %s has no default", key)
	}
	assert.Len(t, defs, len(KnownKeys())-countObjectKeys(objDefs))
}

func countObjectKeys(m map[string]map[string]any) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()
	res, err := Load(Source{})
	require.NoError(t, err)
	c := res.Config
	assert.Equal(t, 8080, c.App.Port)
	assert.Equal(t, 30*time.Second, c.App.RequestTimeout.D())
	assert.Equal(t, 10*365*24*time.Hour, c.Storage.Retention.D())
	assert.Equal(t, 50, c.Log.MaxSize.MB())
	assert.True(t, c.Auth.PublicRead)
	assert.Equal(t, ".", res.Dir)
	assert.Empty(t, res.Warnings)
}

func TestLoadLayers(t *testing.T) {
	t.Parallel()
	path := writeFile(t, siteYAML+"app:\n  port: 9000\n  bogus: 1\n")
	res, err := Load(Source{
		File:    path,
		DB:      map[string]any{"app.port": 7000, "site.name": "DB", "app.log_level": "debug"},
		Environ: []string{"PVMON_APP__LOG_LEVEL=warn", "OTHER=1", "PVMON_DEVICES__WR1__UNIT=3"},
		Getenv:  getenv(map[string]string{"WARP_PASSWORD": "s3cret"}),
	})
	require.NoError(t, err)
	c := res.Config
	assert.Equal(t, 9000, c.App.Port, "file wins over DB")
	assert.Equal(t, "Home", c.Site.Name)
	assert.Equal(t, "warn", c.App.LogLevel, "env wins over file and DB")
	assert.Equal(t, 3, c.Devices["wr1"].Unit)
	assert.Equal(t, 1, c.Devices["warp"].Unit, "object default")
	assert.Equal(t, 5*time.Second, c.Devices["warp"].Interval.D()/2)
	assert.Equal(t, "s3cret", c.Devices["warp"].Auth.Password.Value())
	assert.InDelta(t, 10000.0, c.Meters["battery"].Capacity.Wh(), 0)
	assert.Equal(t, "N", c.Connections["lan-solis"].Parity)
	assert.Equal(t, filepath.Dir(path), res.Dir)
	assert.Contains(t, res.FileKeys, "devices.wr1.profile")
	assert.Equal(t, []string{`unknown or removed key "app.bogus" is ignored`}, res.Warnings)
}

func TestLoadSecretFile(t *testing.T) {
	t.Parallel()
	yml := strings.Replace(siteYAML, "${env:WARP_PASSWORD}", "${file:/run/secrets/warp}", 1)
	res, err := Load(Source{
		File: writeFile(t, yml),
		ReadFile: func(p string) ([]byte, error) {
			if p == "/run/secrets/warp" {
				return []byte("fromfile\n"), nil
			}
			return nil, os.ErrNotExist
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "fromfile", res.Config.Devices["warp"].Auth.Password.Value())
}

func TestLoadProblems(t *testing.T) {
	t.Parallel()
	yml := strings.Replace(siteYAML, "${env:WARP_PASSWORD}", "plain", 1) +
		"app:\n  request_timeout: 30\n"
	_, err := Load(Source{File: writeFile(t, yml)})
	got := problems(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "app.request_timeout: 30: needs a unit", got[0])
	assert.Contains(t, got[1], "devices.warp.auth.password: secrets in")

	_, err = Load(Source{File: writeFile(t, siteYAML), Getenv: getenv(nil)})
	assert.Equal(t, []string{
		"devices.warp.auth.password: config: secret: environment variable WARP_PASSWORD not set",
	}, problems(t, err))

	_, err = Load(Source{File: writeFile(t, strings.Replace(siteYAML,
		"${env:WARP_PASSWORD}", "${file:/nope}", 1)), ReadFile: os.ReadFile})
	assert.Contains(t, problems(t, err)[0], "secret file")
}

func TestLoadData(t *testing.T) {
	t.Parallel()
	res, err := Load(Source{Data: []byte("app: { port: 9001 }\n"), Getenv: getenv(nil)})
	require.NoError(t, err)
	assert.Equal(t, 9001, res.Config.App.Port)
	assert.Contains(t, res.FileKeys, "app.port")
	_, err = Load(Source{Data: []byte("app: [")})
	require.ErrorContains(t, err, "parse data")
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	_, err := Load(Source{File: filepath.Join(t.TempDir(), "missing.yaml")})
	require.Error(t, err)
	_, err = Load(Source{File: writeFile(t, "app: [")})
	require.Error(t, err)
	_, err = Load(Source{File: writeFile(t, "app:\n  port: abc\n")})
	require.ErrorContains(t, err, "decode")
}

func TestValidate(t *testing.T) {
	t.Parallel()
	res, err := Load(Source{
		File:   writeFile(t, siteYAML),
		Getenv: getenv(map[string]string{"WARP_PASSWORD": "x"}),
	})
	require.NoError(t, err)
	c := res.Config
	c.App.Port = 0
	c.Log.RotateAt = []string{"24:00"}
	c.Meters["grid"] = Meter{Device: "meterr", Source: "total"}
	c.Devices["Bad_ID"] = Device{
		Connection: "nope", Address: "x", Read: "fast",
		Netting: "x", Rollover: "7:00", Interval: Duration(time.Millisecond),
	}
	c.Site.Interval = 0
	c.Connections["lan-solis"] = Connection{Parity: "X"}
	rule := func(*Config) Problems { return Problems{{Path: "z.rule", Message: "from rule"}} }
	got := problems(t, Validate(c, rule))
	for _, want := range []string{
		"app.port: must be 1..65535",
		"log.rotate_at.0: \"24:00\" is not HH:MM",
		`meters.grid.device: unknown device "meterr"`,
		"devices.Bad_ID: ID must match [a-z][a-z0-9-]{0,31}",
		"devices.Bad_ID: set exactly one of connection and address",
		`devices.Bad_ID.connection: unknown connection "nope"`,
		"devices.Bad_ID.read: must be range or direct",
		"devices.Bad_ID.interval: must be 0 (site interval) or >= 1s",
		"site.interval: must be >= 1s",
		"connections.lan-solis.address: must not be empty",
		"connections.lan-solis.parity: must be N, E or O",
		"z.rule: from rule",
	} {
		assert.Contains(t, got, want)
	}
	assert.Equal(t, "z.rule: from rule", got[len(got)-1], "sorted by path")
	assert.NoError(t, Problems(nil).Err())
}

func TestParse(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]time.Duration{
		"5s": 5 * time.Second, "30d": 30 * 24 * time.Hour, "1y": 365 * 24 * time.Hour,
		"1h30m": 90 * time.Minute, "0.5d": 12 * time.Hour, "100ms": 100 * time.Millisecond,
	} {
		d, err := ParseDuration(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, d.D(), in)
	}
	for _, in := range []string{"", "5", "5 s", "s5", "5x", "5s5"} {
		_, err := ParseDuration(in)
		assert.Error(t, err, in)
	}
	_, err := durationValue("x", "d")
	require.Error(t, err)
	_, err = durationValue("1", "q")
	require.Error(t, err)
	assert.Equal(t, "1m0s", Duration(time.Minute).String())

	e, err := ParseEnergy("1.5 MWh")
	require.NoError(t, err)
	assert.InDelta(t, 1.5e6, e.Wh(), 0)
	_, err = ParseEnergy("10")
	require.ErrorIs(t, err, ErrNoUnit)

	s, err := ParseSize("512 KB")
	require.NoError(t, err)
	assert.Equal(t, 1, s.MB())
	assert.Equal(t, 0, Size(0).MB())
	_, err = ParseSize("1TB")
	require.Error(t, err)
	_, err = ParseSize("99999999999999999999B")
	require.Error(t, err)
}

func TestSecretMasking(t *testing.T) {
	t.Parallel()
	s := NewSecret("pw")
	assert.True(t, s.IsSet())
	assert.Equal(t, "***", s.String())
	b, err := json.Marshal(s)
	require.NoError(t, err)
	assert.JSONEq(t, `"***"`, string(b))
	txt, err := s.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "***", string(txt))
	assert.Empty(t, Secret{}.String())
	assert.True(t, IsSecretRef("${env:A}"))
	assert.False(t, IsSecretRef("${vault:A}"))
}
