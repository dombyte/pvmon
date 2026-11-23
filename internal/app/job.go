package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dombyte/pvmon/internal/database"
)

// OpenJobDB opens and migrates the database of a "stopped" CLI job; the caller holds the
// exclusive lock and closes the database.
func OpenJobDB(ctx context.Context, l *Loaded, now time.Time) (*sql.DB, error) {
	db, err := database.Open(ctx, l.Result.Config.Storage.Path)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	bs := backupSettings(l)
	backup := func(ctx context.Context) error {
		_, err := database.Backup(ctx, db, bs, now)
		return err
	}
	if err := database.Migrate(ctx, db, backup, now); err != nil {
		return nil, errors.Join(fmt.Errorf("app: %w", err), db.Close())
	}
	return db, nil
}

// BackupJobDB makes the verified backup every CLI job needs before writing (spec
// 7.5) and returns its path.
func BackupJobDB(ctx context.Context, l *Loaded, db *sql.DB, now time.Time) (string, error) {
	path, err := database.Backup(ctx, db, backupSettings(l), now)
	if err != nil {
		return "", fmt.Errorf("app: %w", err)
	}
	return path, nil
}
