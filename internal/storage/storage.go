// Package storage is the sole owner of the SQLite database. It enforces the write domains,
// writes buckets together with their counter state and invalidates derived values of past
// slots in the same transaction (spec 7.3, 7.5).
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/dombyte/pvmon/internal/util"
)

// Storage wraps the database. Only internal/app holds it; everybody else gets one of the
// domain interfaces.
type Storage struct {
	db  *sql.DB
	loc *time.Location // site time zone for local periods
	gen atomic.Int64   // derived cache generation; bumped by every invalidation
}

// New wraps an opened and migrated database.
func New(db *sql.DB, loc *time.Location) (*Storage, error) {
	if err := util.RequireAll("storage", util.D("db", db), util.D("location", loc)); err != nil {
		return nil, err
	}
	return &Storage{db: db, loc: loc}, nil
}

// Collector returns the collector's write interface.
func (s *Storage) Collector() CollectorStore { return &collectorStore{s: s} }

// Derive returns the derive write interface (derived cache only).
func (s *Storage) Derive() DeriveStore { return &deriveStore{s: s} }

// Read returns the read interface.
func (s *Storage) Read() ReadStore { return &readStore{s: s} }

// domainTables lists the tables each domain may write (spec 7.5).
func domainTables() map[Domain][]string {
	return map[Domain][]string{
		DomainCollector: {"series", "b15_gauge", "b15_energy", "counter_state", "status_event"},
		DomainDerive:    {"derived_cache", "carry"},
		DomainConfig:    {"config_object", "config_setting", "config_history", "audit_log"},
		DomainAuth:      {"user", "session", "api_token", "audit_log"},
	}
}

// allowed reports whether domain may write table; CLI jobs may write everything.
func allowed(d Domain, table string) bool {
	if d == DomainCLI {
		return true
	}
	for _, t := range domainTables()[d] {
		if t == table {
			return true
		}
	}
	return false
}

// write runs fn in one transaction after checking that d may write every table.
func (s *Storage) write(ctx context.Context, d Domain, tables []string,
	fn func(tx *sql.Tx) error,
) error {
	for _, t := range tables {
		if !allowed(d, t) {
			return &DomainError{Domain: d, Table: t}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin: %w", err)
	}
	if err := fn(tx); err != nil {
		return errors.Join(err, rollback(tx))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit: %w", err)
	}
	return nil
}

func rollback(tx *sql.Tx) error {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("storage: rollback: %w", err)
	}
	return nil
}

// invalidate deletes the derived cache keys of the day, month, year and lifetime that
// contain each slot before todayStart (spec 7.3). It runs inside the writer's transaction.
func (s *Storage) invalidate(ctx context.Context, tx *sql.Tx, slots []int64, todayStart int64,
) error {
	days := map[string]bool{}
	for _, slot := range slots {
		if slot < todayStart {
			days[time.Unix(slot*SlotSeconds, 0).In(s.loc).Format(time.DateOnly)] = true
		}
	}
	if len(days) == 0 {
		return nil
	}
	// A value computed before this write must not be stored after it (see CachePut).
	s.gen.Add(1)
	_, err := tx.ExecContext(ctx, `DELETE FROM derived_cache WHERE key LIKE '%|total%'`)
	if err != nil {
		return fmt.Errorf("storage: invalidate total: %w", err)
	}
	for day := range days {
		_, err := tx.ExecContext(ctx, `DELETE FROM derived_cache WHERE key LIKE ? OR key LIKE ?
			OR key LIKE ?`, "%|day|"+day, "%|month|"+day[:7], "%|year|"+day[:4])
		if err != nil {
			return fmt.Errorf("storage: invalidate %s: %w", day, err)
		}
	}
	return nil
}
