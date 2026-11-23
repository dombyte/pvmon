package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/database"
)

// importEnv writes a config with a Solis inverter, an import map and the solis fixture of
// internal/maintenance and returns their paths.
func importEnv(t *testing.T, mapYAML string) (cfg, mapFile, solis string) {
	t.Helper()
	dir := t.TempDir()
	cfg = importConfig(t, dir)
	mapFile = filepath.Join(dir, "map.yaml")
	require.NoError(t, os.WriteFile(mapFile, []byte(mapYAML), 0o600))
	solis = filepath.Join(dir, "solis.db")
	ctx := context.Background()
	db, err := database.Open(ctx, solis)
	require.NoError(t, err)
	script, err := os.ReadFile(filepath.Join("..", "internal", "maintenance", "testdata",
		"solis.sql"))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(script))
	require.NoError(t, err)
	require.NoError(t, db.Close())
	return cfg, mapFile, solis
}

// importConfig writes dir/config.yaml with its database at dir/data/pvmon.db.
func importConfig(t *testing.T, dir string) string {
	t.Helper()
	yaml := fmt.Sprintf(`
storage: { path: %q }
connections:
  lan: { address: "tcp://127.0.0.1:1", timeout: 1s }
devices:
  wr1: { connection: lan, unit: 1, profile: solis/s6-hybrid }
meters:
  pv:      { device: wr1, source: pv }
  battery: { device: wr1, source: battery }
  grid:    { device: wr1, source: grid }
`, filepath.Join(dir, "data", "pvmon.db"))
	cfg := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte(yaml), 0o600))
	return cfg
}

const importMap = `
energy: { pv_energy: pv/energy_out, battery_charge: battery/energy_in }
status: { device: wr1, registers: { solis_status: operating } }
`

func TestImportSolis(t *testing.T) {
	t.Parallel()
	cfg, mapFile, solis := importEnv(t, importMap)
	code, out, errOut := runArgs("import-solis", "-config", cfg, "--db", solis, "--map", mapFile)
	require.Equal(t, exitOK, code, errOut)
	for _, want := range []string{
		"backup: ",
		"pv_energy              -> pv/energy_out           5 days      33.75 kWh, carry 10.00 kWh",
		"battery_charge         -> battery/energy_in       1 days       4.00 kWh, " +
			"1 negative days skipped, solis total below daily sum (no carry)",
		"not imported (not in map): household_energy",
		"status changes: 3",
		"status not imported (not in map): mystery_fault",
		"monthly pv_energy              2026-02 solis 150.00 kWh, daily sum 0.00 kWh (0 days)",
	} {
		assert.Contains(t, out, want)
	}

	code, _, errOut = runArgs("import-solis", "-config", cfg, "--db", solis, "--map", mapFile)
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut, "already holds an import")
}

func TestImportSolisUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"import-solis"},
		{"import-solis", "--db", "x.db"},
		{"import-solis", "--map", "m.yaml"},
		{"import-solis", "--db", "x.db", "--map", "m.yaml", "extra"},
		{"import-solis", "--bogus"},
	} {
		code, _, _ := runArgs(args...)
		assert.Equal(t, exitUsage, code, args)
	}
}

func TestImportSolisFailures(t *testing.T) {
	t.Parallel()
	cfg, mapFile, solis := importEnv(t, importMap)
	_, badMap, _ := importEnv(t, "energy: { pv_energy: nowhere/energy_out }")
	_, junkMap, _ := importEnv(t, "energy: [")
	other := filepath.Join(t.TempDir(), "other.db")
	db, err := database.Open(context.Background(), other)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"config", []string{
			"-config", filepath.Join(t.TempDir(), "none.yaml"), "--db", solis,
			"--map", mapFile,
		}, "config"},
		{
			"map missing",
			[]string{"-config", cfg, "--db", solis, "--map", "/nonexistent.yaml"},
			"import map",
		},
		{
			"map syntax",
			[]string{"-config", cfg, "--db", solis, "--map", junkMap},
			"invalid import map",
		},
		{
			"map target",
			[]string{"-config", cfg, "--db", solis, "--map", badMap},
			"no energy series",
		},
		{
			"solis missing",
			[]string{"-config", cfg, "--db", "/nonexistent.db", "--map", mapFile},
			"open /nonexistent.db",
		},
		{
			"not solis",
			[]string{"-config", cfg, "--db", other, "--map", mapFile},
			"not a solis database",
		},
	}
	for _, tc := range cases {
		code, _, errOut := runArgs(append([]string{"import-solis"}, tc.args...)...)
		assert.Equal(t, exitFailure, code, tc.name)
		assert.Contains(t, errOut, tc.want, tc.name)
	}
}

func TestImportSolisLockFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A regular file where the data directory should be makes the lock setup fail.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data"), nil, 0o600))
	_, mapFile, solis := importEnv(t, importMap)
	code, _, errOut := runArgs("import-solis", "-config", importConfig(t, dir), "--db", solis,
		"--map", mapFile)
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut, "data directory")
}
