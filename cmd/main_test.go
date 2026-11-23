package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runArgs(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, nil, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersion(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"version"}, {"-version"}} {
		code, out, _ := runArgs(args...)
		assert.Equal(t, exitOK, code)
		assert.Contains(t, out, "pvmon dev")
	}
	code, _, errOut := runArgs("version", "x")
	assert.Equal(t, exitUsage, code)
	assert.Contains(t, errOut, "usage:")
}

func TestUnknownCommand(t *testing.T) {
	t.Parallel()
	code, _, errOut := runArgs("frobnicate")
	assert.Equal(t, exitUsage, code)
	assert.Contains(t, errOut, `unknown command "frobnicate"`)
}

func TestServeDefault(t *testing.T) {
	t.Parallel()
	// meters without a grid meter; an empty site (defaults only) would start the server
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(
		"devices: { m: { address: \"http://10.0.0.31\", profile: shelly/pro-3em } }\n"+
			"meters: { load: { device: m, source: total, role: consumer } }\n"), 0o600))
	code, _, errOut := runArgs("-config", path)
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut, "exactly one grid meter")
	code, _, _ = runArgs("-config", "missing.yaml")
	assert.Equal(t, exitFailure, code)
	code, _, _ = runArgs("serve", "stray")
	assert.Equal(t, exitUsage, code)
}

func TestPanicExitsOne(t *testing.T) {
	t.Parallel()
	var errOut bytes.Buffer
	code := run(context.Background(), []string{"version"}, nil, nil, &errOut)
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut.String(), "panic")
}

func TestProfileValidate(t *testing.T) {
	t.Parallel()
	code, out, _ := runArgs("profile", "validate", "../profiles/goe/charger-v2.yaml")
	assert.Equal(t, exitOK, code)
	assert.Contains(t, out, "ok (goe/charger-v2 v2)")

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(bad, []byte("schema: 2\nprofile: x/y\n"), 0o600))
	code, out, _ = runArgs("profile", "validate", bad, filepath.Join(t.TempDir(), "none.yaml"))
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, out, "schema: must be 1")
	assert.Contains(t, out, "none.yaml: open")

	code, _, _ = runArgs("profile", "plan")
	assert.Equal(t, exitUsage, code)
}
