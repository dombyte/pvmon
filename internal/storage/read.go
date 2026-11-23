package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

type readStore struct{ s *Storage }

// SumEnergy sums one series over slots [from, to) in one SQL query; flags are ORed.
func (r *readStore) SumEnergy(ctx context.Context, seriesID, from, to int64,
) (EnergyTotal, error) {
	var (
		t  EnergyTotal
		wh int64
	)
	err := r.s.db.QueryRowContext(ctx, `SELECT coalesce(sum(wh), 0), count(*),
		coalesce(max(flags & 1), 0) | coalesce(max(flags & 2), 0) |
		coalesce(max(flags & 4), 0) | coalesce(max(flags & 8), 0)
		FROM b15_energy WHERE series_id=? AND slot>=? AND slot<?`, seriesID, from, to,
	).Scan(&wh, &t.Buckets, &t.Flags)
	if err != nil {
		return EnergyTotal{}, fmt.Errorf("storage: sum energy: %w", err)
	}
	t.Wh = fromFixed(wh)
	return t, nil
}

// EnergyBuckets returns the energy buckets of a series in [from, to), ordered by slot.
func (r *readStore) EnergyBuckets(ctx context.Context, seriesID, from, to int64,
) ([]EnergyBucket, error) {
	rows, err := r.s.db.QueryContext(ctx, `SELECT series_id, slot, wh, counter, flags
		FROM b15_energy WHERE series_id=? AND slot>=? AND slot<? ORDER BY slot`,
		seriesID, from, to)
	if err != nil {
		return nil, fmt.Errorf("storage: energy buckets: %w", err)
	}
	defer rows.Close()
	var out []EnergyBucket
	for rows.Next() {
		var (
			b       EnergyBucket
			wh      int64
			counter sql.NullInt64
		)
		if err := rows.Scan(&b.SeriesID, &b.Slot, &wh, &counter, &b.Flags); err != nil {
			return nil, fmt.Errorf("storage: scan energy: %w", err)
		}
		b.Wh, b.Counter = fromFixed(wh), fromFixedNull(counter)
		out = append(out, b)
	}
	return out, wrapRows(rows.Err())
}

// GaugeBuckets returns the gauge buckets of a series in [from, to), ordered by slot.
func (r *readStore) GaugeBuckets(ctx context.Context, seriesID, from, to int64,
) ([]GaugeBucket, error) {
	rows, err := r.s.db.QueryContext(ctx, `SELECT series_id, slot, avg, min, max, cover
		FROM b15_gauge WHERE series_id=? AND slot>=? AND slot<? ORDER BY slot`,
		seriesID, from, to)
	if err != nil {
		return nil, fmt.Errorf("storage: gauge buckets: %w", err)
	}
	defer rows.Close()
	var out []GaugeBucket
	for rows.Next() {
		var (
			g           GaugeBucket
			avg, lo, hi int64
		)
		if err := rows.Scan(&g.SeriesID, &g.Slot, &avg, &lo, &hi, &g.Cover); err != nil {
			return nil, fmt.Errorf("storage: scan gauge: %w", err)
		}
		g.Avg, g.Min, g.Max = fromFixed(avg), fromFixed(lo), fromFixed(hi)
		out = append(out, g)
	}
	return out, wrapRows(rows.Err())
}

// SeriesID returns the active series of a meter channel.
func (r *readStore) SeriesID(ctx context.Context, key SeriesKey) (int64, error) {
	var id int64
	err := r.s.db.QueryRowContext(ctx, `SELECT id FROM series WHERE meter_id=? AND channel=?
		AND active_to IS NULL`, key.Meter, key.Channel).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: series %s/%s", ErrNotFound, key.Meter, key.Channel)
	}
	if err != nil {
		return 0, fmt.Errorf("storage: series id: %w", err)
	}
	return id, nil
}

// FirstSlot returns the earliest slot of any energy or gauge bucket (two MIN queries on the
// primary keys); ok is false when nothing is stored yet.
func (r *readStore) FirstSlot(ctx context.Context) (int64, bool, error) {
	var slot sql.NullInt64
	err := r.s.db.QueryRowContext(ctx, `SELECT min(m) FROM (
		SELECT min(slot) AS m FROM b15_energy UNION ALL SELECT min(slot) FROM b15_gauge)`,
	).Scan(&slot)
	if err != nil {
		return 0, false, fmt.Errorf("storage: first slot: %w", err)
	}
	return slot.Int64, slot.Valid, nil
}

// maxInList caps the variables of one IN list, far below SQLite's variable limit.
const maxInList = 500

// chunks splits items into slices of at most maxInList elements.
func chunks[T any](items []T) [][]T {
	var out [][]T
	for len(items) > maxInList {
		out = append(out, items[:maxInList])
		items = items[maxInList:]
	}
	if len(items) > 0 {
		out = append(out, items)
	}
	return out
}

// SumEnergyMany sums several series over slots [from, to), one grouped query per chunk.
func (r *readStore) SumEnergyMany(ctx context.Context, ids []int64, from, to, closedBefore int64,
) (map[int64]EnergySum, error) {
	out := make(map[int64]EnergySum, len(ids))
	for _, part := range chunks(ids) {
		//nolint:gosec // only "?" placeholders are concatenated; all values are bound
		q := `SELECT series_id, sum(wh), count(*),
			coalesce(sum(slot < ? AND (flags & 2) = 0), 0),
			coalesce(max((flags & 3) != 0), 0), coalesce(max((flags & 8) != 0), 0)
			FROM b15_energy WHERE series_id IN (` + placeholders(len(part)) + `)
			AND slot>=? AND slot<? GROUP BY series_id`
		args := append([]any{closedBefore}, int64Args(part)...)
		rows, err := r.s.db.QueryContext(ctx, q, append(args, from, to)...)
		if err != nil {
			return nil, fmt.Errorf("storage: sum energy many: %w", err)
		}
		err = scanEnergySums(rows, out)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func scanEnergySums(rows *sql.Rows, out map[int64]EnergySum) error {
	defer rows.Close()
	for rows.Next() {
		var (
			id  int64
			wh  int64
			sum EnergySum
			est int
			imp int
		)
		if err := rows.Scan(&id, &wh, &sum.Buckets, &sum.Closed, &est, &imp); err != nil {
			return fmt.Errorf("storage: scan energy sum: %w", err)
		}
		sum.Wh = fromFixed(wh)
		sum.Estimated = est != 0
		sum.Imported = imp != 0
		out[id] = sum
	}
	return wrapRows(rows.Err())
}

// Carry returns the carried energy per series ID; keys that are no series IDs are ignored.
func (r *readStore) Carry(ctx context.Context, ids []int64) (map[int64]float64, error) {
	out := make(map[int64]float64, len(ids))
	for _, part := range chunks(ids) {
		keys := make([]any, len(part))
		for i, id := range part {
			keys[i] = strconv.FormatInt(id, 10)
		}
		//nolint:gosec // only "?" placeholders are concatenated; all values are bound
		rows, err := r.s.db.QueryContext(ctx, `SELECT key, wh FROM carry WHERE key IN (`+
			placeholders(len(part))+`)`, keys...)
		if err != nil {
			return nil, fmt.Errorf("storage: carry: %w", err)
		}
		if err := scanCarry(rows, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func scanCarry(rows *sql.Rows, out map[int64]float64) error {
	defer rows.Close()
	for rows.Next() {
		var (
			key string
			wh  float64
		)
		if err := rows.Scan(&key, &wh); err != nil {
			return fmt.Errorf("storage: scan carry: %w", err)
		}
		if id, err := strconv.ParseInt(key, 10, 64); err == nil {
			out[id] = wh
		}
	}
	return wrapRows(rows.Err())
}

// StatusEvents returns the status changes of a device with from <= at < to (unix ms),
// newest first and at most limit, plus per register the change active at from (spec 9.1).
func (r *readStore) StatusEvents(ctx context.Context, device string, from, to int64,
	limit int,
) ([]StatusEvent, error) {
	in, err := r.statusQuery(ctx, `SELECT device_id, register, at, code FROM status_event
		WHERE device_id=? AND at>=? AND at<? ORDER BY at DESC, register LIMIT ?`,
		device, from, to, limit)
	if err != nil {
		return nil, err
	}
	before, err := r.statusQuery(ctx, `SELECT device_id, register, max(at), code
		FROM status_event WHERE device_id=? AND at<? GROUP BY register ORDER BY register`,
		device, from)
	if err != nil {
		return nil, err
	}
	return append(in, before...), nil
}

func (r *readStore) statusQuery(ctx context.Context, q string, args ...any,
) ([]StatusEvent, error) {
	rows, err := r.s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: status events: %w", err)
	}
	defer rows.Close()
	var out []StatusEvent
	for rows.Next() {
		var e StatusEvent
		if err := rows.Scan(&e.Device, &e.Register, &e.At, &e.Code); err != nil {
			return nil, fmt.Errorf("storage: scan status: %w", err)
		}
		out = append(out, e)
	}
	return out, wrapRows(rows.Err())
}
