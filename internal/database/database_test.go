package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func openTemp(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pvmon.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func noBackup(context.Context) error { return errors.New("must not back up") }

func TestMigrateFresh(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := openTemp(t)
	v, err := Version(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, 0, v)
	require.NoError(t, Migrate(ctx, db, noBackup, t0))
	v, err = Version(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, LatestVersion(), v)
	assert.Equal(t, 5, v)
	require.NoError(t, Migrate(ctx, db, noBackup, t0), "idempotent")
	var created string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT value FROM meta WHERE key='created_at'`).Scan(&created))
	assert.Equal(t, "1795089600", created)
	var mode string
	require.NoError(t, db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode))
	assert.Equal(t, "wal", mode)
}

// Migration 4 turns REAL bucket values into milli-unit integers and drops the cache.
func TestMigrateFixedPointBuckets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := openTemp(t)
	ms, err := loadMigrations()
	require.NoError(t, err)
	require.NoError(t, migrate(ctx, db, ms[:3], noBackup, t0))
	_, err = db.ExecContext(ctx, `
		INSERT INTO b15_gauge VALUES (1, 10, 738.09523809523807, 690.0, 800.0005, 105);
		INSERT INTO b15_energy VALUES (2, 10, 5.22768973477279, 27400.000000000004, 1),
			(2, 11, 0.0, NULL, 0);
		INSERT INTO derived_cache VALUES ('x|day|2022-01-01', 0, '{}');`)
	require.NoError(t, err)
	require.NoError(t, Migrate(ctx, db, func(context.Context) error { return nil }, t0))

	var avg, lo, hi, cover int64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT avg, min, max, cover FROM b15_gauge`).Scan(&avg, &lo, &hi, &cover))
	assert.Equal(t, []int64{738095, 690000, 800001, 105}, []int64{avg, lo, hi, cover})
	rows, err := db.QueryContext(ctx, `SELECT wh, counter, typeof(wh) FROM b15_energy ORDER BY slot`)
	require.NoError(t, err)
	defer rows.Close()
	var got []string
	for rows.Next() {
		var (
			wh      int64
			counter sql.NullInt64
			typ     string
		)
		require.NoError(t, rows.Scan(&wh, &counter, &typ))
		got = append(got, fmt.Sprintf("%d %t %d %s", wh, counter.Valid, counter.Int64, typ))
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"5228 true 27400000 integer", "0 false 0 integer"}, got)
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM derived_cache`).Scan(&n))
	assert.Zero(t, n)
}

func TestMigrateTooNewAndBackup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := openTemp(t)
	require.NoError(t, Migrate(ctx, db, noBackup, t0))
	_, err := db.ExecContext(ctx, `UPDATE meta SET value='99' WHERE key='schema_version'`)
	require.NoError(t, err)
	require.ErrorIs(t, Migrate(ctx, db, noBackup, t0), ErrTooNew)

	_, err = db.ExecContext(ctx, `UPDATE meta SET value='1' WHERE key='schema_version'`)
	require.NoError(t, err)
	ms := []migration{{1, "SELECT 1"}, {2, "CREATE TABLE extra (x INTEGER)"}}
	backups := 0
	ok := func(context.Context) error { backups++; return nil }
	require.NoError(t, migrate(ctx, db, ms, ok, t0))
	assert.Equal(t, 1, backups, "non-empty database is backed up first")
	v, _ := Version(ctx, db)
	assert.Equal(t, 2, v)

	ms = append(ms, migration{3, "CREATE TABLE extra (x)"})
	require.ErrorContains(t, migrate(ctx, db, ms, ok, t0), "migration 3")
	ms = append(ms[:2], migration{3, "SELECT 1"})
	require.ErrorContains(t, migrate(ctx, db, ms, noBackup, t0), "backup before migration")

	_, err = db.ExecContext(ctx, `UPDATE meta SET value='x' WHERE key='schema_version'`)
	require.NoError(t, err)
	_, err = Version(ctx, db)
	require.Error(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM meta`)
	require.NoError(t, err)
	v, err = Version(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, 0, v)
}

func TestOpenErrors(t *testing.T) {
	t.Parallel()
	_, err := Open(context.Background(), filepath.Join(t.TempDir(), "no", "dir", "x.db"))
	require.Error(t, err)
}

func TestOpenReadOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, path := openTemp(t)
	_, err := db.ExecContext(ctx, `CREATE TABLE t (v INTEGER); INSERT INTO t VALUES (7)`)
	require.NoError(t, err)
	ro, err := OpenReadOnly(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ro.Close() })
	var v int
	require.NoError(t, ro.QueryRowContext(ctx, `SELECT v FROM t`).Scan(&v))
	assert.Equal(t, 7, v)
	_, err = ro.ExecContext(ctx, `INSERT INTO t VALUES (8)`)
	require.Error(t, err, "read-only")
	_, err = OpenReadOnly(ctx, filepath.Join(t.TempDir(), "missing.db"))
	require.Error(t, err, "a missing file is not created")
}

func TestBackup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, path := openTemp(t)
	require.NoError(t, Migrate(ctx, db, noBackup, t0))
	dir := filepath.Join(filepath.Dir(path), "backups")
	key := filepath.Join(filepath.Dir(path), "secret.key")
	s := BackupSettings{Dir: dir, KeyFile: key, MaxBackups: 2}

	first, err := Backup(ctx, db, s, t0)
	require.NoError(t, err, "missing key file is skipped")
	assert.NoFileExists(t, first[:len(first)-3]+".secret.key")
	require.NoError(t, os.WriteFile(key, []byte("k"), 0o600))
	for i := 1; i <= 3; i++ {
		_, err = Backup(ctx, db, s, t0.Add(time.Duration(i)*time.Hour))
		require.NoError(t, err)
	}
	list, err := List(dir)
	require.NoError(t, err)
	require.Len(t, list, 2, "pruned to max_backups")
	assert.Contains(t, list[0], "20261119-150000")
	assert.FileExists(t, list[0][:len(list[0])-3]+".secret.key")
	newest, err := Newest(dir)
	require.NoError(t, err)
	assert.Equal(t, t0.Add(3*time.Hour), newest)
	none, err := Newest(t.TempDir())
	require.NoError(t, err)
	assert.True(t, none.IsZero())

	same, err := Backup(ctx, db, s, t0.Add(3*time.Hour+time.Millisecond))
	require.NoError(t, err, "two jobs in the same second get distinct files")
	assert.Contains(t, same, "20261119-150000.001")
	_, err = Backup(ctx, db, s, t0.Add(3*time.Hour+time.Millisecond))
	require.Error(t, err, "VACUUM INTO an existing file fails")
	_, err = Backup(ctx, db, BackupSettings{Dir: "/proc/nope"}, t0)
	require.Error(t, err)
	_, err = Backup(ctx, db, BackupSettings{Dir: dir, KeyFile: dir}, t0.Add(5*time.Hour))
	require.ErrorContains(t, err, "key")
}

func TestNewestLegacyName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pvmon-20261119-120000.db"), nil, 0o600))
	newest, err := Newest(dir)
	require.NoError(t, err)
	assert.Equal(t, t0, newest)
}

func TestVerify(t *testing.T) {
	t.Parallel()
	bad := filepath.Join(t.TempDir(), "bad.db")
	require.NoError(t, os.WriteFile(bad, []byte("not a database at all, definitely"), 0o600))
	require.ErrorIs(t, Verify(context.Background(), bad), ErrBackupInvalid)
	require.ErrorIs(t, Verify(context.Background(), filepath.Join(t.TempDir(), "x", "y.db")),
		ErrBackupInvalid)
	require.NoError(t, removeBackup(filepath.Join(t.TempDir(), "gone")))
	require.Error(t, Prune("[", 1))
}

func TestParseMigrations(t *testing.T) {
	t.Parallel()
	_, err := parseMigrations(fstest.MapFS{"migrations/x_init.sql": {Data: []byte("")}})
	require.ErrorContains(t, err, "bad version")
	_, err = parseMigrations(fstest.MapFS{"migrations/0001_a.sql": {Mode: fs.ModeDir}})
	require.ErrorContains(t, err, "read")
	ms, err := parseMigrations(fstest.MapFS{
		"migrations/0002_b.sql": {Data: []byte("b")},
		"migrations/0001_a.sql": {Data: []byte("a")},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, ms[0].version)
}

func TestMigrateClosed(t *testing.T) {
	t.Parallel()
	db, _ := openTemp(t)
	require.NoError(t, db.Close())
	require.Error(t, Migrate(context.Background(), db, noBackup, t0))
	require.Error(t, applyOne(context.Background(), db, migration{1, "SELECT 1"}, true, t0))
}
