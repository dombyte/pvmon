package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// Config returns the interface of the config service.
func (s *Storage) Config() ConfigStore { return &configStore{s: s} }

type configStore struct{ s *Storage }

// ConfigState reads the UI configuration and its revision.
func (c *configStore) ConfigState(ctx context.Context) (ConfigState, error) {
	st := ConfigState{Settings: map[string]string{}}
	rev, err := configRev(ctx, c.s.db)
	if err != nil {
		return st, err
	}
	st.Rev = rev
	if st.Objects, err = configObjects(ctx, c.s.db); err != nil {
		return st, err
	}
	rows, err := c.s.db.QueryContext(ctx, `SELECT key, value FROM config_setting ORDER BY key`)
	if err != nil {
		return st, fmt.Errorf("storage: config settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return st, fmt.Errorf("storage: config settings: %w", err)
		}
		st.Settings[k] = v
	}
	if err := rows.Err(); err != nil {
		return st, fmt.Errorf("storage: config settings: %w", err)
	}
	return st, nil
}

// querier is the read part shared by *sql.DB and *sql.Tx.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func configRev(ctx context.Context, q querier) (int64, error) {
	var rev int64
	err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(rev),0) FROM config_history`).Scan(&rev)
	if err != nil {
		return 0, fmt.Errorf("storage: config rev: %w", err)
	}
	return rev, nil
}

func configObjects(ctx context.Context, q querier) ([]ConfigObject, error) {
	rows, err := q.QueryContext(ctx, `SELECT kind, id, body, updated_at,
		COALESCE(updated_by,'') FROM config_object ORDER BY kind, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: config objects: %w", err)
	}
	defer rows.Close()
	var out []ConfigObject
	for rows.Next() {
		var o ConfigObject
		if err := rows.Scan(&o.Kind, &o.ID, &o.Body, &o.UpdatedAt, &o.UpdatedBy); err != nil {
			return nil, fmt.Errorf("storage: config objects: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: config objects: %w", err)
	}
	return out, nil
}

// ApplyConfig writes one UI change with a new revision; ErrConflict when another change
// was saved since BaseRev.
func (c *configStore) ApplyConfig(ctx context.Context, ch ConfigChange) (int64, error) {
	var rev int64
	tables := []string{"config_object", "config_setting", "config_history", "audit_log"}
	err := c.s.write(ctx, DomainConfig, tables, func(tx *sql.Tx) error {
		cur, err := configRev(ctx, tx)
		if err != nil {
			return err
		}
		if cur != ch.BaseRev {
			return ErrConflict
		}
		if err := applyObjects(ctx, tx, ch); err != nil {
			return err
		}
		if err := applySettings(ctx, tx, ch); err != nil {
			return err
		}
		rev = cur + 1
		if _, err := tx.ExecContext(ctx, `INSERT INTO config_history(rev, at, by, snapshot)
			VALUES(?,?,NULLIF(?,''),?)`, rev, ch.At, ch.By, ch.Snapshot); err != nil {
			return fmt.Errorf("storage: config history: %w", err)
		}
		return insertAudit(ctx, tx, ch.Audit)
	})
	return rev, err
}

func applyObjects(ctx context.Context, tx *sql.Tx, ch ConfigChange) error {
	for _, k := range ch.Delete {
		if _, err := tx.ExecContext(ctx, `DELETE FROM config_object WHERE kind=? AND id=?`,
			k.Kind, k.ID); err != nil {
			return fmt.Errorf("storage: delete config %s %s: %w", k.Kind, k.ID, err)
		}
	}
	for _, o := range ch.Put {
		if _, err := tx.ExecContext(ctx, `INSERT INTO config_object(kind, id, body, updated_at,
			updated_by) VALUES(?,?,?,?,NULLIF(?,'')) ON CONFLICT(kind, id) DO UPDATE SET
			body=excluded.body, updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
			o.Kind, o.ID, o.Body, o.UpdatedAt, o.UpdatedBy); err != nil {
			return fmt.Errorf("storage: put config %s %s: %w", o.Kind, o.ID, err)
		}
	}
	return nil
}

func applySettings(ctx context.Context, tx *sql.Tx, ch ConfigChange) error {
	for _, k := range ch.Unset {
		if _, err := tx.ExecContext(ctx, `DELETE FROM config_setting WHERE key=?`, k); err != nil {
			return fmt.Errorf("storage: unset %s: %w", k, err)
		}
	}
	for k, v := range ch.Set {
		if _, err := tx.ExecContext(ctx, `INSERT INTO config_setting(key, value) VALUES(?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v); err != nil {
			return fmt.Errorf("storage: set %s: %w", k, err)
		}
	}
	return nil
}
