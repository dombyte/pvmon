package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/database"
	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

func TestBackupJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(dir, "pvmon.db"))
	require.NoError(t, err)
	defer db.Close()
	clock := clocktest.New(time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	j := &backupJob{db: db, s: database.BackupSettings{
		Dir:        filepath.Join(dir, "b"),
		MaxBackups: 2,
	}, interval: 24 * time.Hour, clock: clock, log: zerolog.Nop()}
	j.maybeBackup(ctx)
	j.maybeBackup(ctx) // not due yet
	list, err := database.List(filepath.Join(dir, "b"))
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, health.Healthy, j.State())

	j.s.Dir = "/proc/pvmon-backups"
	j.maybeBackup(ctx)
	assert.Equal(t, health.Recovering, j.State())
	require.NoError(t, (&backupJob{}).Stop())
}
