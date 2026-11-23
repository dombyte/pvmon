package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// metaImportSource is the meta key that records an import (spec 7.1, 7.8).
const metaImportSource = "import_source"

// Import returns the write interface of the import CLI job.
func (s *Storage) Import() ImportStore { return &importStore{s: s} }

type importStore struct{ s *Storage }

func (i *importStore) ImportSource(ctx context.Context) (string, error) {
	var v string
	err := i.s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`,
		metaImportSource).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: import source: %w", err)
	}
	return v, nil
}

func (i *importStore) FirstSlot(ctx context.Context) (int64, bool, error) {
	return (&readStore{s: i.s}).FirstSlot(ctx)
}

func (i *importStore) Import(ctx context.Context, b ImportBatch) error {
	tables := []string{
		"meta", "series", "b15_energy", "carry", "status_event", "derived_cache",
	}
	return i.s.write(ctx, DomainCLI, tables, func(tx *sql.Tx) error {
		var done string
		err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`,
			metaImportSource).Scan(&done)
		if err == nil {
			return fmt.Errorf("%w (%s)", ErrAlreadyImported, done)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("storage: import source: %w", err)
		}
		if err := importAll(ctx, tx, b); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM derived_cache`)
		if err != nil {
			return fmt.Errorf("storage: drop derived cache: %w", err)
		}
		return nil
	})
}

func importAll(ctx context.Context, tx *sql.Tx, b ImportBatch) error {
	for _, s := range b.Series {
		if err := importSeries(ctx, tx, s); err != nil {
			return err
		}
	}
	if err := insertStatus(ctx, tx, b.Status); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?)`,
		metaImportSource, b.Source)
	if err != nil {
		return fmt.Errorf("storage: record import: %w", err)
	}
	return nil
}

// importSeries ensures the series, lowers its active_from to the first imported slot and
// inserts buckets and carry; an occupied slot fails the whole import.
func importSeries(ctx context.Context, tx *sql.Tx, s ImportSeries) error {
	if len(s.Buckets) == 0 {
		return nil
	}
	first := firstBucket(s.Buckets)
	id, err := ensureOne(ctx, tx, first, s.Def)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE series SET active_from = min(active_from, ?)
		WHERE id = ?`, first, id)
	if err != nil {
		return fmt.Errorf("storage: series %d active_from: %w", id, err)
	}
	for _, e := range s.Buckets {
		if err := insertEnergy(ctx, tx, id, e); err != nil {
			return err
		}
	}
	return insertCarry(ctx, tx, id, s.Carry, first)
}

func firstBucket(bs []EnergyBucket) int64 {
	first := bs[0].Slot
	for _, e := range bs {
		first = min(first, e.Slot)
	}
	return first
}

// insertCarry stores the energy before slot first (spec 7.4); 0 stores nothing.
func insertCarry(ctx context.Context, tx *sql.Tx, id int64, wh float64, first int64) error {
	if wh == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO carry(key, wh, before_slot) VALUES(?, ?, ?)`,
		strconv.FormatInt(id, 10), wh, first)
	if err != nil {
		return fmt.Errorf("storage: carry %d: %w", id, err)
	}
	return nil
}

func insertEnergy(ctx context.Context, tx *sql.Tx, id int64, e EnergyBucket) error {
	wh, err := toFixed(e.Wh)
	if err != nil {
		return fmt.Errorf("storage: import %d/%d: %w", id, e.Slot, err)
	}
	counter, err := toFixedPtr(e.Counter)
	if err != nil {
		return fmt.Errorf("storage: import counter %d/%d: %w", id, e.Slot, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO b15_energy(series_id, slot, wh, counter, flags)
		VALUES(?,?,?,?,?)`, id, e.Slot, wh, counter, e.Flags)
	if err != nil {
		return fmt.Errorf("storage: import bucket %d/%d: %w", id, e.Slot, err)
	}
	return nil
}

func insertStatus(ctx context.Context, tx *sql.Tx, events []StatusEvent) error {
	for _, e := range events {
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO status_event(device_id,
			register, at, code) VALUES(?,?,?,?)`, e.Device, e.Register, e.At, e.Code)
		if err != nil {
			return fmt.Errorf("storage: import status %s/%s: %w", e.Device, e.Register, err)
		}
	}
	return nil
}
