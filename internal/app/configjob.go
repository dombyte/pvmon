package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/dombyte/pvmon/internal/database"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
)

// LoadStored returns l with the UI configuration stored in its database, opened
// read-only so it works while the server runs. Without a database file l is returned.
func LoadStored(ctx context.Context, l *Loaded, clock util.Clock) (*Loaded, error) {
	path := l.Result.Config.Storage.Path
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	db, err := database.OpenReadOnly(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	defer db.Close() // read-only, nothing to flush
	st, err := storage.New(db, time.UTC)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	cs, err := CreateConfigStore(l, st, clock, false)
	if err != nil {
		return nil, err
	}
	snap, err := cs.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	return l.WithDB(snap.Layer(), snap.Rev)
}
