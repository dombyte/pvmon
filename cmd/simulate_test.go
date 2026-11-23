package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSimulation(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"R1", "R3", "R4"} {
		l, err := loadSimulation(s, defaultConfigPath)
		require.NoError(t, err, s)
		assert.Contains(t, l.Result.Config.Storage.Path, "simulate.db")
	}
	_, err := loadSimulation("R9", defaultConfigPath)
	require.ErrorIs(t, err, errUsage)
	l, err := loadSimulation("", defaultConfigPath)
	require.NoError(t, err, "defaults alone are an empty site (first start)")
	assert.Empty(t, l.Site.Devices)
}

func TestSimulateCommand(t *testing.T) {
	t.Parallel()
	code, _, _ := runArgs("simulate", "stray")
	assert.Equal(t, exitUsage, code)
	code, _, _ = runArgs("simulate", "--setup", "R9")
	assert.Equal(t, exitUsage, code)
}
