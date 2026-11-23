package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStdout(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l, err := New(&buf, Settings{Level: "warn"})
	require.NoError(t, err)
	l.Info().Msg("hidden")
	l.Warn().Str("component", "x").Msg("shown")
	require.NoError(t, l.Close())
	assert.NotContains(t, buf.String(), "hidden")
	assert.Contains(t, buf.String(), `"component":"x"`)
}

func TestNewFile(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	path := filepath.Join(t.TempDir(), "pvmon.log")
	l, err := New(&buf, Settings{
		Level: "info", File: path, MaxSizeMB: 1, MaxBackups: 1,
		MaxAge: 48 * time.Hour, Compression: "none",
	})
	require.NoError(t, err)
	l.Info().Msg("both")
	require.NoError(t, l.Close())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "both")
	assert.Contains(t, buf.String(), "both")
}

func TestNewInvalidLevel(t *testing.T) {
	t.Parallel()
	for _, lvl := range []string{"", "loud"} {
		_, err := New(&bytes.Buffer{}, Settings{Level: lvl})
		require.Error(t, err)
	}
}
