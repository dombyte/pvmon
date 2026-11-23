package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // driver for the stored UI configuration
)

func TestConfigValidateAndExportWithoutDB(t *testing.T) {
	t.Parallel()
	cfg := importConfig(t, t.TempDir())

	code, out, errOut := cli(t, nil, "config", "validate", "-config", cfg)
	require.Equal(t, exitOK, code, errOut)
	assert.Equal(t, "configuration valid (config_rev 0)\n", out)

	code, out, errOut = cli(t, nil, "config", "export", "-config", cfg)
	require.Equal(t, exitOK, code, errOut)
	assert.Contains(t, out, "solis/s6-hybrid")
	assert.NotContains(t, out, "site:")
}

func TestConfigWithStoredLayer(t *testing.T) {
	t.Parallel()
	cfg := importConfig(t, t.TempDir())
	// A user job creates and migrates the database.
	code, _, errOut := cli(t, strings.NewReader("correct horse battery\n"),
		"user", "add", "admin", "-config", cfg)
	require.Equal(t, exitOK, code, errOut)

	db, err := sql.Open("sqlite", filepath.Join(filepath.Dir(cfg), "data", "pvmon.db"))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO config_setting (key, value) VALUES ('site.name', '"Stored"');
		INSERT INTO config_history (rev, at, by, snapshot) VALUES (3, 0, 'test', '{}')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	code, out, errOut := cli(t, nil, "config", "validate", "-config", cfg)
	require.Equal(t, exitOK, code, errOut)
	assert.Contains(t, out, "config_rev 3")
	code, out, errOut = cli(t, nil, "config", "export", "-config", cfg)
	require.Equal(t, exitOK, code, errOut)
	assert.Contains(t, out, "Stored")
}

func TestConfigErrors(t *testing.T) {
	t.Parallel()
	cfg := importConfig(t, t.TempDir())
	for name, args := range map[string][]string{
		"no subcommand":      {"config"},
		"unknown subcommand": {"config", "frob", "-config", cfg},
		"extra args":         {"config", "validate", "extra", "-config", cfg},
		"bad flag":           {"config", "export", "-nope"},
	} {
		code, _, _ := cli(t, nil, args...)
		assert.Equal(t, exitUsage, code, name)
	}

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(bad, []byte("meters:\n  grid: { device: ghost }\n"), 0o600))
	code, _, errOut := cli(t, nil, "config", "validate", "-config", bad)
	assert.Equal(t, exitFailure, code)
	assert.NotEmpty(t, errOut)

	code, _, _ = cli(t, nil, "config", "validate", "-config", filepath.Join(t.TempDir(), "x"))
	assert.Equal(t, exitFailure, code, "missing file")
}

func TestLoadSimulationStoragePath(t *testing.T) {
	t.Parallel()
	cfg := importConfig(t, t.TempDir())
	l, err := loadSimulation("", cfg)
	require.NoError(t, err)
	assert.Equal(t, "simulate.db", filepath.Base(l.Result.Config.Storage.Path))
	assert.Equal(t, "data", filepath.Base(filepath.Dir(l.Result.Config.Storage.Path)))
}
