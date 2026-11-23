package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type collectorStore struct{ s *Storage }

const maxCover = SlotSeconds

// EnsureSeries returns the IDs of the active series, creating missing ones from slot on.
func (c *collectorStore) EnsureSeries(ctx context.Context, slot int64, defs []SeriesDef,
) (map[SeriesKey]int64, error) {
	out := make(map[SeriesKey]int64, len(defs))
	err := c.s.write(ctx, DomainCollector, []string{"series"}, func(tx *sql.Tx) error {
		for _, d := range defs {
			id, err := ensureOne(ctx, tx, slot, d)
			if err != nil {
				return err
			}
			out[d.SeriesKey] = id
		}
		return nil
	})
	return out, err
}

func ensureOne(ctx context.Context, tx *sql.Tx, slot int64, d SeriesDef) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM series WHERE meter_id=? AND channel=?
		AND active_to IS NULL`, d.Meter, d.Channel).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("storage: find series %s/%s: %w", d.Meter, d.Channel, err)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO series(meter_id, channel, kind, origin, hidden,
		unit, active_from) VALUES(?,?,?,?,?,?,?)`, d.Meter, d.Channel, d.Kind, d.Origin,
		boolInt(d.Hidden), d.Unit, slot)
	if err != nil {
		return 0, fmt.Errorf("storage: create series %s/%s: %w", d.Meter, d.Channel, err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: series id: %w", err)
	}
	return id, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CounterStates loads the persisted counter states of ids; missing ids are absent.
func (c *collectorStore) CounterStates(ctx context.Context, ids []int64,
) (map[int64]CounterState, error) {
	out := map[int64]CounterState{}
	if len(ids) == 0 {
		return out, nil
	}
	in := placeholders(len(ids))
	//nolint:gosec // only "?" placeholders are concatenated; all values are bound
	q := `SELECT series_id, raw, at, remainder, coalesce(resolution, 0), reset,
		seen FROM counter_state
		WHERE series_id IN (` + in + `)`
	rows, err := c.s.db.QueryContext(ctx, q, int64Args(ids)...)
	if err != nil {
		return nil, fmt.Errorf("storage: counter states: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var st CounterState
		if err := rows.Scan(&st.SeriesID, &st.Raw, &st.At, &st.Remainder, &st.Resolution,
			&st.ResetAt, &st.Seen); err != nil {
			return nil, fmt.Errorf("storage: scan counter state: %w", err)
		}
		out[st.SeriesID] = st
	}
	return out, wrapRows(rows.Err())
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func int64Args(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func wrapRows(err error) error {
	if err != nil {
		return fmt.Errorf("storage: rows: %w", err)
	}
	return nil
}

// Flush writes gauge and energy buckets and counter states in one transaction; energy is
// added to stored buckets, gauges are merged by covered seconds (spec 5.1, 5.3).
func (c *collectorStore) Flush(ctx context.Context, b Batch) error {
	tables := []string{"b15_gauge", "b15_energy", "counter_state"}
	return c.s.write(ctx, DomainCollector, tables, func(tx *sql.Tx) error {
		var slots []int64
		for _, g := range b.Gauges {
			if err := upsertGauge(ctx, tx, g); err != nil {
				return err
			}
			slots = append(slots, g.Slot)
		}
		for _, e := range b.Energy {
			if err := upsertEnergy(ctx, tx, e); err != nil {
				return err
			}
			slots = append(slots, e.Slot)
		}
		if err := saveCounters(ctx, tx, b.Counters); err != nil {
			return err
		}
		return c.s.invalidate(ctx, tx, slots, b.TodayStart)
	})
}

func upsertGauge(ctx context.Context, tx *sql.Tx, g GaugeBucket) error {
	vals, err := fixedAll(g.Avg, g.Min, g.Max)
	if err != nil {
		return fmt.Errorf("storage: gauge %d/%d: %w", g.SeriesID, g.Slot, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO b15_gauge(series_id, slot, avg, min, max, cover)
		VALUES(?,?,?,?,?,?) ON CONFLICT(series_id, slot) DO UPDATE SET
		avg = CASE WHEN cover + excluded.cover > 0
			THEN CAST(round((avg * cover + excluded.avg * excluded.cover) * 1.0
				/ (cover + excluded.cover)) AS INTEGER)
			ELSE excluded.avg END,
		min = min(min, excluded.min), max = max(max, excluded.max),
		cover = min(?, cover + excluded.cover)`,
		g.SeriesID, g.Slot, vals[0], vals[1], vals[2], g.Cover, maxCover)
	if err != nil {
		return fmt.Errorf("storage: upsert gauge %d/%d: %w", g.SeriesID, g.Slot, err)
	}
	return nil
}

func upsertEnergy(ctx context.Context, tx *sql.Tx, e EnergyBucket) error {
	wh, err := toFixed(e.Wh)
	if err != nil {
		return fmt.Errorf("storage: energy %d/%d: %w", e.SeriesID, e.Slot, err)
	}
	counter, err := toFixedPtr(e.Counter)
	if err != nil {
		return fmt.Errorf("storage: counter %d/%d: %w", e.SeriesID, e.Slot, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO b15_energy(series_id, slot, wh, counter, flags)
		VALUES(?,?,?,?,?) ON CONFLICT(series_id, slot) DO UPDATE SET
		wh = wh + excluded.wh, counter = coalesce(excluded.counter, counter),
		flags = flags | excluded.flags`,
		e.SeriesID, e.Slot, wh, counter, e.Flags)
	if err != nil {
		return fmt.Errorf("storage: upsert energy %d/%d: %w", e.SeriesID, e.Slot, err)
	}
	return nil
}

func saveCounters(ctx context.Context, tx *sql.Tx, states []CounterState) error {
	for _, st := range states {
		_, err := tx.ExecContext(ctx, `INSERT INTO counter_state(series_id, raw, at, remainder,
			resolution, reset, seen) VALUES(?,?,?,?,?,?,?) ON CONFLICT(series_id) DO UPDATE SET
			raw=excluded.raw, at=excluded.at, remainder=excluded.remainder,
			resolution=excluded.resolution, reset=excluded.reset, seen=excluded.seen`,
			st.SeriesID, st.Raw, st.At, st.Remainder, st.Resolution, st.ResetAt, st.Seen)
		if err != nil {
			return fmt.Errorf("storage: save counter %d: %w", st.SeriesID, err)
		}
	}
	return nil
}

// AddStatus stores status changes.
func (c *collectorStore) AddStatus(ctx context.Context, events []StatusEvent) error {
	return c.s.write(ctx, DomainCollector, []string{"status_event"}, func(tx *sql.Tx) error {
		for _, e := range events {
			_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO status_event(device_id,
				register, at, code) VALUES(?,?,?,?)`, e.Device, e.Register, e.At, e.Code)
			if err != nil {
				return fmt.Errorf("storage: add status %s/%s: %w", e.Device, e.Register, err)
			}
		}
		return nil
	})
}

// LastStatus returns the newest event per register of a device.
func (c *collectorStore) LastStatus(ctx context.Context, device string,
) (map[string]StatusEvent, error) {
	rows, err := c.s.db.QueryContext(ctx, `SELECT device_id, register, max(at), code
		FROM status_event WHERE device_id=? GROUP BY register`, device)
	if err != nil {
		return nil, fmt.Errorf("storage: last status: %w", err)
	}
	defer rows.Close()
	out := map[string]StatusEvent{}
	for rows.Next() {
		var e StatusEvent
		if err := rows.Scan(&e.Device, &e.Register, &e.At, &e.Code); err != nil {
			return nil, fmt.Errorf("storage: scan status: %w", err)
		}
		out[e.Register] = e
	}
	return out, wrapRows(rows.Err())
}
