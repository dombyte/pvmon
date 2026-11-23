// Package database opens the SQLite database, applies the embedded, versioned migrations
// (always after a verified backup) and makes verified online backups (spec 7.6).
package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the cgo-free "sqlite" driver
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrTooNew means the database was written by a newer pvmon.
var ErrTooNew = errors.New("database: schema is newer than this binary")

// Open opens path with WAL, foreign keys and a single connection.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	q := url.Values{}
	for _, p := range []string{
		"journal_mode(WAL)", "foreign_keys(1)", "busy_timeout(5000)", "synchronous(NORMAL)",
	} {
		q.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("database: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close() // already failing; the ping error is the one to report
		return nil, fmt.Errorf("database: open %s: %w", path, err)
	}
	return db, nil
}

// OpenReadOnly opens an existing database file read-only (mode=ro), e.g. the solis
// database of an import; a missing file fails instead of creating one.
func OpenReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("database: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close() // already failing; the ping error is the one to report
		return nil, fmt.Errorf("database: open %s: %w", path, err)
	}
	return db, nil
}

// migration is one embedded migration file NNNN_name.sql.
type migration struct {
	version int
	sql     string
}

func loadMigrations() ([]migration, error) { return parseMigrations(migrations) }

func parseMigrations(fsys fs.FS) ([]migration, error) {
	files, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("database: list migrations: %w", err)
	}
	out := make([]migration, 0, len(files))
	for _, f := range files {
		base := strings.TrimPrefix(f, "migrations/")
		v, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0]) //nolint:mnd // prefix
		if err != nil {
			return nil, fmt.Errorf("database: migration %s: bad version", f)
		}
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, fmt.Errorf("database: read %s: %w", f, err)
		}
		out = append(out, migration{version: v, sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// LatestVersion returns the schema version of this binary.
func LatestVersion() int {
	ms, err := loadMigrations()
	if err != nil {
		return 0 // the embedded migrations are fixed at build time and tested
	}
	return ms[len(ms)-1].version
}

// Version returns the schema version stored in meta (0 for an empty database).
func Version(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='meta'`).Scan(&n)
	if err != nil || n == 0 {
		return 0, wrap("read schema", err)
	}
	var v string
	err = db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='schema_version'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, wrap("read schema_version", err)
	}
	ver, err := strconv.Atoi(v)
	return ver, wrap("parse schema_version", err)
}

func wrap(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("database: %s: %w", what, err)
}

// BackupFunc makes and verifies a backup before a migration of a non-empty database.
type BackupFunc func(ctx context.Context) error

// Migrate brings the schema to LatestVersion. A non-empty database is backed up first; a
// newer database is refused with ErrTooNew.
func Migrate(ctx context.Context, db *sql.DB, backup BackupFunc, now time.Time) error {
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	return migrate(ctx, db, ms, backup, now)
}

func migrate(ctx context.Context, db *sql.DB, ms []migration, backup BackupFunc,
	now time.Time,
) error {
	cur, err := Version(ctx, db)
	if err != nil {
		return err
	}
	latest := ms[len(ms)-1].version
	switch {
	case cur > latest:
		return fmt.Errorf("%w (database %d, binary %d)", ErrTooNew, cur, latest)
	case cur == latest:
		return nil
	case cur > 0:
		if err := backup(ctx); err != nil {
			return fmt.Errorf("database: backup before migration: %w", err)
		}
	}
	return apply(ctx, db, ms, cur, now)
}

func apply(ctx context.Context, db *sql.DB, ms []migration, cur int, now time.Time) error {
	for _, m := range ms {
		if m.version <= cur {
			continue
		}
		if err := applyOne(ctx, db, m, cur == 0, now); err != nil {
			return err
		}
		cur = m.version
	}
	return nil
}

func applyOne(ctx context.Context, db *sql.DB, m migration, fresh bool, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return wrap("begin migration", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("database: migration %d: %w", m.version, err)
	}
	stmts := []string{`INSERT INTO meta(key, value) VALUES('schema_version', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`}
	args := [][]any{{strconv.Itoa(m.version)}}
	if fresh {
		stmts = append(stmts, `INSERT OR IGNORE INTO meta(key, value) VALUES('created_at', ?)`)
		args = append(args, []any{strconv.FormatInt(now.Unix(), 10)})
	}
	for i, s := range stmts {
		if _, err := tx.ExecContext(ctx, s, args[i]...); err != nil {
			return fmt.Errorf("database: migration %d meta: %w", m.version, err)
		}
	}
	return wrap("commit migration", tx.Commit())
}
