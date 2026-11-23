package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/profiles"
)

// TestExampleConfig keeps example/config.yaml valid against the schema and the profiles.
func TestExampleConfig(t *testing.T) {
	t.Parallel()
	getenv := func(k string) (string, bool) { return "secret", k == "WARP_PASSWORD" }
	l, err := app.LoadConfig(config.Source{
		File: "../../example/config.yaml", Getenv: getenv,
	}, profiles.FS())
	require.NoError(t, err)
	assert.Empty(t, l.Result.Warnings)
	caps := l.Site.Capabilities()
	assert.Equal(t, 1, caps.PV, "the strings sit below the PV total")
	assert.Equal(t, 2, caps.Loadpoints["wallbox"])
}
