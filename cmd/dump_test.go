package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
)

const goeBody = `{"nrg":[231,229,230,1,16,16,16,3696,3664,3680,0,11040,0,0,0,0],
"eto":8765432,"wh":7200,"car":2,"err":0,"modelStatus":15}`

// dumpConfig writes a config.yaml with a Shelly grid meter and a go-e charger, both served
// by srvURL, and returns its path.
func dumpConfig(t *testing.T, srvURL string) string {
	t.Helper()
	dir := t.TempDir()
	yaml := fmt.Sprintf(`
storage: { path: %q }
devices:
  meter: { address: %q, profile: shelly/pro-3em }
  goe:   { address: %q, profile: goe/charger-v2 }
meters:
  grid: { device: meter, source: total }
  wb:   { device: goe, source: charger }
`, filepath.Join(dir, "data", "pvmon.db"), srvURL, srvURL)
	p := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(yaml), 0o600))
	return p
}

func goeServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(goeBody))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDumpUsageErrors(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"no device":  {"dump"},
		"count zero": {"dump", "--device", "goe", "--count", "0"},
		"stray args": {"dump", "--device", "goe", "extra"},
		"bad flag":   {"dump", "--nope"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code, _, errOut := runArgs(args...)
			assert.Equal(t, exitUsage, code)
			assert.Contains(t, errOut, "usage:")
		})
	}
}

func TestDumpUnknownDevice(t *testing.T) {
	t.Parallel()
	cfg := dumpConfig(t, goeServer(t).URL)
	code, _, errOut := runArgs("dump", "-config", cfg, "--device", "nope")
	assert.Equal(t, exitUsage, code)
	assert.Contains(t, errOut, `unknown device "nope"`)
}

func TestDumpConfigError(t *testing.T) {
	t.Parallel()
	bad := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(bad, []byte("devices: [\n"), 0o600))
	code, _, errOut := runArgs("dump", "-config", bad, "--device", "goe")
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut, "config:")
}

func TestDumpToFile(t *testing.T) {
	t.Parallel()
	cfg := dumpConfig(t, goeServer(t).URL)
	out := filepath.Join(t.TempDir(), "goe.json")
	code, _, errOut := runArgs("dump", "-config", cfg, "--device", "goe", "--count", "1",
		"-o", out)
	require.Equal(t, exitOK, code, errOut)

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	var fx profile.Fixture
	require.NoError(t, json.Unmarshal(data, &fx))
	assert.Equal(t, "dump", fx.Source)
	assert.Equal(t, "goe/charger-v2", fx.Profile)
	require.Len(t, fx.Samples, 1)
	assert.Empty(t, fx.Samples[0].Err)
	assert.Contains(t, fx.Samples[0].JSON, "status")
}

func TestDumpToStdout(t *testing.T) {
	t.Parallel()
	cfg := dumpConfig(t, goeServer(t).URL)
	code, out, errOut := runArgs("dump", "-config", cfg, "--device", "goe")
	require.Equal(t, exitOK, code, errOut)
	assert.Contains(t, out, `"source": "dump"`)
}

func TestDumpOutputNotWritable(t *testing.T) {
	t.Parallel()
	cfg := dumpConfig(t, goeServer(t).URL)
	out := filepath.Join(t.TempDir(), "missing", "goe.json")
	code, _, errOut := runArgs("dump", "-config", cfg, "--device", "goe", "-o", out)
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut, "dump output")
}

func TestDumpLockHeld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A regular file where the data directory should be makes the lock setup fail.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data"), nil, 0o600))
	yaml := fmt.Sprintf(`
storage: { path: %q }
devices:
  meter: { address: "http://127.0.0.1:1", profile: shelly/pro-3em }
meters:
  grid: { device: meter, source: total }
`, filepath.Join(dir, "data", "pvmon.db"))
	cfg := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte(yaml), 0o600))
	code, _, errOut := runArgs("dump", "-config", cfg, "--device", "meter")
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut, "data directory")
}

func TestDumpModbusDeviceUnreachable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := fmt.Sprintf(`
storage: { path: %q }
connections:
  lan: { address: "tcp://127.0.0.1:1", timeout: 100ms }
devices:
  wr1: { connection: lan, unit: 1, profile: solis/s6-hybrid }
meters:
  grid: { device: wr1, source: grid, role: grid }
`, filepath.Join(dir, "pvmon.db"))
	cfg := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte(yaml), 0o600))
	// The deadline bounds the poll: an unreachable device is recorded as a failed sample.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"dump", "-config", cfg, "--device", "wr1", "--interval", "1s"}, nil,
		&out, &errOut)
	require.Equal(t, exitOK, code, errOut.String())
	assert.Contains(t, out.String(), `"err"`)
}
