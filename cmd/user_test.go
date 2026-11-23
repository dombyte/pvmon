package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cli runs pvmon with stdin and returns exit code, stdout and stderr.
func cli(t *testing.T, stdin io.Reader, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, stdin, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestUserCommands(t *testing.T) {
	t.Parallel()
	cfg := importConfig(t, t.TempDir())
	pw := strings.NewReader

	code, _, errOut := cli(t, pw("short\n"), "user", "add", "admin", "-config", cfg)
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut, "too_short")
	assert.NotContains(t, errOut, "backup:", "a weak password is rejected before the backup")

	code, _, errOut = cli(t, pw(""), "user", "add", "admin", "-config", cfg)
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut, "no password on stdin")
	code, _, _ = cli(t, nil, "user", "add", "admin", "-config", cfg)
	assert.Equal(t, exitFailure, code)

	code, out, errOut := cli(t, pw("correct horse battery\n"), "user", "add", "admin",
		"--lang", "de", "-config", cfg)
	require.Equal(t, exitOK, code, errOut)
	assert.Contains(t, out, "user admin created (id 1)")
	assert.Contains(t, errOut, "backup: ")

	code, _, errOut = cli(t, pw("correct horse battery\n"), "user", "add", "ADMIN",
		"-config", cfg)
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut, "already taken")

	code, out, _ = cli(t, nil, "user", "list", "-config", cfg)
	require.Equal(t, exitOK, code)
	assert.Contains(t, out, "admin")
	assert.Contains(t, out, "last login never")

	code, out, errOut = cli(t, pw("another long password\n"), "user", "reset-password",
		"admin", "-config", cfg)
	require.Equal(t, exitOK, code, errOut)
	assert.Contains(t, out, "password of admin changed")
	code, _, errOut = cli(t, pw("another long password\n"), "user", "reset-password",
		"ghost", "-config", cfg)
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut, "not found")
}

func TestTokenCommands(t *testing.T) {
	t.Parallel()
	cfg := importConfig(t, t.TempDir())

	code, out, errOut := cli(t, nil, "token", "create", "--name", "ha", "--expires", "90d",
		"-config", cfg)
	require.Equal(t, exitOK, code, errOut)
	tok := strings.TrimSpace(out)
	assert.True(t, strings.HasPrefix(tok, "pvm_"), tok)
	assert.Contains(t, errOut, "shown only once")

	code, out, _ = cli(t, nil, "token", "list", "-config", cfg)
	require.Equal(t, exitOK, code)
	id := tok[4:12]
	assert.Contains(t, out, id)
	assert.Contains(t, out, "last used never")
	assert.NotContains(t, out, tok, "the secret is never listed")

	code, out, _ = cli(t, nil, "token", "revoke", id, "-config", cfg)
	require.Equal(t, exitOK, code)
	assert.Contains(t, out, "revoked")
	code, _, errOut = cli(t, nil, "token", "revoke", id, "-config", cfg)
	assert.Equal(t, exitFailure, code)
	assert.Contains(t, errOut, "not found")
}

func TestAuthCommandUsage(t *testing.T) {
	t.Parallel()
	cfg := importConfig(t, t.TempDir())
	for _, args := range [][]string{
		{"user"},
		{"user", "bogus"},
		{"user", "add"},
		{"user", "list", "extra"},
		{"user", "list", "-nope"},
		{"token"},
		{"token", "create"},
		{"token", "create", "--name", "x", "--expires", "5"},
		{"token", "create", "--name", "x", "--expires", "-1d"},
		{"token", "revoke"},
	} {
		code, _, _ := cli(t, nil, append(args, "-config", cfg)...)
		assert.Equal(t, exitUsage, code, args)
	}
}
