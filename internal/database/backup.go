package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backup file naming: pvmon-<UTC timestamp>.db plus .secret.key next to it.
const (
	backupPrefix = "pvmon-"
	backupExt    = ".db"
	keyExt       = ".secret.key"
	// stampLayout has milliseconds so two CLI jobs in the same second get distinct files;
	// legacyLayout is the name of backups written before.
	stampLayout  = "20060102-150405.000"
	legacyLayout = "20060102-150405"
	dirMode      = 0o750
	fileMode     = 0o600
)

// ErrBackupInvalid means integrity_check of a new backup failed; the file was deleted.
var ErrBackupInvalid = errors.New("database: backup failed integrity check")

// BackupSettings locates backups and the secret key that belongs to them.
type BackupSettings struct {
	Dir        string // backup directory
	KeyFile    string // data/secret.key; skipped while it does not exist
	MaxBackups int
}

// Backup writes a verified online backup (VACUUM INTO + integrity_check), copies the
// secret key next to it and prunes old backups. It returns the backup path.
func Backup(ctx context.Context, db *sql.DB, s BackupSettings, now time.Time) (string, error) {
	if err := os.MkdirAll(s.Dir, dirMode); err != nil {
		return "", fmt.Errorf("database: backup dir: %w", err)
	}
	path := filepath.Join(s.Dir, backupPrefix+now.UTC().Format(stampLayout)+backupExt)
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return "", fmt.Errorf("database: vacuum into %s: %w", path, err)
	}
	if err := Verify(ctx, path); err != nil {
		return "", errors.Join(err, removeBackup(path))
	}
	if err := copyKey(s.KeyFile, strings.TrimSuffix(path, backupExt)+keyExt); err != nil {
		return "", errors.Join(err, removeBackup(path))
	}
	return path, Prune(s.Dir, s.MaxBackups)
}

// Verify runs PRAGMA integrity_check on a database file.
func Verify(ctx context.Context, path string) error {
	db, err := Open(ctx, path)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBackupInvalid, err)
	}
	defer db.Close()
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
		return fmt.Errorf("%w: %w", ErrBackupInvalid, err)
	}
	if res != "ok" {
		return fmt.Errorf("%w: %s", ErrBackupInvalid, res)
	}
	return nil
}

func copyKey(src, dst string) error {
	in, err := os.Open(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("database: open key: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("database: write key copy: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		return errors.Join(fmt.Errorf("database: copy key: %w", err), out.Close())
	}
	return wrap("close key copy", out.Close())
}

func removeBackup(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("database: remove backup: %w", err)
	}
	return nil
}

// List returns the backups in dir, newest first.
func List(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, backupPrefix+"*"+backupExt))
	if err != nil {
		return nil, fmt.Errorf("database: list backups: %w", err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	return matches, nil
}

// Newest returns the time of the newest backup, zero without backups.
func Newest(dir string) (time.Time, error) {
	list, err := List(dir)
	if err != nil || len(list) == 0 {
		return time.Time{}, err
	}
	stamp := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(list[0]), backupPrefix),
		backupExt)
	t, err := time.Parse(stampLayout, stamp)
	if err != nil {
		t, err = time.Parse(legacyLayout, stamp)
	}
	return t, wrap("parse backup time", err)
}

// Prune deletes all but the newest keep backups and their key copies.
func Prune(dir string, keep int) error {
	list, err := List(dir)
	if err != nil {
		return err
	}
	var errs []error
	for i := max(keep, 1); i < len(list); i++ {
		errs = append(errs, removeBackup(list[i]),
			removeBackup(strings.TrimSuffix(list[i], backupExt)+keyExt))
	}
	return errors.Join(errs...)
}
