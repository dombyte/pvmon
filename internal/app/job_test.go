package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/database"
)

func TestJobDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	l := loadServeConfig(t)
	now := time.Date(2026, 11, 20, 12, 31, 7, 0, time.UTC)
	require.NoError(t, os.MkdirAll(filepath.Dir(l.Result.Config.Storage.Path), 0o750))
	db, err := app.OpenJobDB(ctx, l, now)
	require.NoError(t, err)
	v, err := database.Version(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, database.LatestVersion(), v, "migrated")
	path, err := app.BackupJobDB(ctx, l, db, now)
	require.NoError(t, err)
	assert.FileExists(t, path)
	_, err = app.BackupJobDB(ctx, l, db, now)
	require.Error(t, err, "the same backup name twice")
	require.NoError(t, db.Close())
}

func TestJobDBErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	l := loadServeConfig(t)
	path := l.Result.Config.Storage.Path
	require.NoError(t, os.MkdirAll(filepath.Join(path, "x"), 0o750))
	_, err := app.OpenJobDB(ctx, l, time.Now())
	require.Error(t, err, "a directory is not a database")

	l = loadServeConfig(t)
	path = l.Result.Config.Storage.Path
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	db, err := database.Open(ctx, path)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO meta VALUES ('schema_version', '999')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	_, err = app.OpenJobDB(ctx, l, time.Now())
	require.ErrorIs(t, err, database.ErrTooNew)
}
