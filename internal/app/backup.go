package app

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/database"
	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/util"
)

// backupCheck is how often the backup job checks whether a backup is due.
const backupCheck = time.Hour

// backupJob makes a verified online backup every interval, counted from the newest
// existing backup (spec 7.6). It is a supervised component.
type backupJob struct {
	db       *sql.DB
	s        database.BackupSettings
	interval time.Duration
	clock    util.Clock
	log      zerolog.Logger

	beat   atomic.Int64
	failed atomic.Bool
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (b *backupJob) Start(ctx context.Context) error {
	b.beat.Store(b.clock.Now().UnixNano())
	ctx, b.cancel = context.WithCancel(ctx)
	b.wg.Add(1)
	go b.run(ctx)
	return nil
}

func (b *backupJob) run(ctx context.Context) {
	defer b.wg.Done()
	t := b.clock.NewTicker(backupCheck)
	defer t.Stop()
	for {
		b.beat.Store(b.clock.Now().UnixNano())
		b.maybeBackup(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}

func (b *backupJob) maybeBackup(ctx context.Context) {
	now := b.clock.Now()
	newest, err := database.Newest(b.s.Dir)
	if err == nil && now.Sub(newest) < b.interval {
		return
	}
	path, err := database.Backup(ctx, b.db, b.s, now)
	b.failed.Store(err != nil)
	if err != nil {
		b.log.Error().Err(err).Msg("backup failed")
		return
	}
	b.log.Info().Str("path", path).Msg("backup written")
}

func (b *backupJob) Stop() error {
	if b.cancel != nil {
		b.cancel()
	}
	b.wg.Wait()
	return nil
}

func (b *backupJob) State() health.State {
	if b.failed.Load() {
		return health.Recovering
	}
	return health.Healthy
}

func (b *backupJob) LastBeat() time.Time { return time.Unix(0, b.beat.Load()) }
