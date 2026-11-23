package storage

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
)

type deriveStore struct{ s *Storage }

// CacheGet returns the cached values of keys stored under configRev; other keys are absent.
func (d *deriveStore) CacheGet(ctx context.Context, keys []string, configRev int64,
) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, part := range chunks(keys) {
		args := []any{configRev}
		for _, k := range part {
			args = append(args, k)
		}
		//nolint:gosec // only "?" placeholders are concatenated; all values are bound
		rows, err := d.s.db.QueryContext(ctx, `SELECT key, value FROM derived_cache
			WHERE config_rev=? AND key IN (`+placeholders(len(part))+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("storage: cache get: %w", err)
		}
		if err := scanCache(rows, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func scanCache(rows *sql.Rows, out map[string]string) error {
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return fmt.Errorf("storage: scan cache: %w", err)
		}
		out[k] = v
	}
	return wrapRows(rows.Err())
}

// CacheGen returns the cache generation; pass it to CachePut before reading the buckets.
func (d *deriveStore) CacheGen() int64 { return d.s.gen.Load() }

// CachePut upserts all entries under configRev in one transaction. It stores nothing when a
// past slot was written since gen was taken: the entries may predate that write. The check
// runs inside the write transaction, and the single connection serialises it with flushes.
func (d *deriveStore) CachePut(ctx context.Context, configRev, gen int64,
	entries map[string]string,
) error {
	if len(entries) == 0 {
		return nil
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return d.s.write(ctx, DomainDerive, []string{"derived_cache"}, func(tx *sql.Tx) error {
		if d.s.gen.Load() != gen {
			return nil
		}
		for _, k := range keys {
			_, err := tx.ExecContext(ctx, `INSERT INTO derived_cache(key, config_rev, value)
				VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET config_rev=excluded.config_rev,
				value=excluded.value`, k, configRev, entries[k])
			if err != nil {
				return fmt.Errorf("storage: cache put %s: %w", k, err)
			}
		}
		return nil
	})
}
