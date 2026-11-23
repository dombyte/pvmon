package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Auth returns the write interface of the auth service and the user/token CLI jobs.
func (s *Storage) Auth() AuthStore { return &authStore{s: s} }

type authStore struct{ s *Storage }

// userCols are the selected columns of a User, in scanUser order.
const userCols = `id, name, pw_hash, COALESCE(lang,''), created_at, COALESCE(last_login,0)`

// tokenCols are the selected columns of an APIToken, in scanToken order.
const tokenCols = `id, secret_hash, name, scope, COALESCE(user_id,0), created_at,
	COALESCE(expires_at,0), COALESCE(last_used,0)`

func (a *authStore) write(ctx context.Context, tables []string, fn func(tx *sql.Tx) error,
) error {
	return a.s.write(ctx, DomainAuth, tables, fn)
}

// CountUsers returns the number of users.
func (a *authStore) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := a.s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user`).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: count users: %w", err)
	}
	return n, nil
}

// CreateUser inserts u and its audit entry; a taken name (case-insensitive) is ErrConflict.
func (a *authStore) CreateUser(ctx context.Context, u User, onlyFirst bool, e AuditEntry,
) (int64, error) {
	var id int64
	err := a.write(ctx, []string{"user", "audit_log"}, func(tx *sql.Tx) error {
		if err := checkNewUser(ctx, tx, u.Name, onlyFirst); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO user(name, pw_hash, lang, created_at)
			VALUES(?,?,NULLIF(?,''),?)`, u.Name, u.PwHash, u.Lang, u.CreatedAt)
		if err != nil {
			return fmt.Errorf("storage: create user: %w", err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return fmt.Errorf("storage: create user: %w", err)
		}
		return insertAudit(ctx, tx, e)
	})
	return id, err
}

func checkNewUser(ctx context.Context, tx *sql.Tx, name string, onlyFirst bool) error {
	q := `SELECT COUNT(*) FROM user WHERE name=?`
	args := []any{name}
	if onlyFirst {
		q, args = `SELECT COUNT(*) FROM user`, nil
	}
	var n int
	if err := tx.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return fmt.Errorf("storage: check user: %w", err)
	}
	if n > 0 {
		return ErrConflict
	}
	return nil
}

// UserByName returns the user with name (case-insensitive) or ErrNotFound.
func (a *authStore) UserByName(ctx context.Context, name string) (User, error) {
	return scanUser(a.s.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM user WHERE name=?`, name))
}

// UserByID returns the user with id or ErrNotFound.
func (a *authStore) UserByID(ctx context.Context, id int64) (User, error) {
	return scanUser(a.s.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM user WHERE id=?`, id))
}

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.PwHash, &u.Lang, &u.CreatedAt, &u.LastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("storage: scan user: %w", err)
	}
	return u, nil
}

// Users returns all users ordered by name.
func (a *authStore) Users(ctx context.Context) ([]User, error) {
	rows, err := a.s.db.QueryContext(ctx, `SELECT `+userCols+` FROM user ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("storage: users: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, wrapRows(rows.Err())
}

// DeleteUser removes the user; sessions and tokens follow by ON DELETE CASCADE.
func (a *authStore) DeleteUser(ctx context.Context, id int64, e AuditEntry) error {
	tables := []string{"user", "session", "api_token", "audit_log"}
	return a.write(ctx, tables, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM user`).Scan(&n); err != nil {
			return fmt.Errorf("storage: count users: %w", err)
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM user WHERE id=?`, id)
		if err != nil {
			return fmt.Errorf("storage: delete user: %w", err)
		}
		if err := affected(res); err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastUser
		}
		return insertAudit(ctx, tx, e)
	})
}

func affected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("storage: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPassword replaces the hash and logs the user out everywhere (spec 10).
func (a *authStore) SetPassword(ctx context.Context, id int64, hash string, e AuditEntry,
) error {
	return a.write(ctx, []string{"user", "session", "audit_log"}, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE user SET pw_hash=? WHERE id=?`, hash, id)
		if err != nil {
			return fmt.Errorf("storage: set password: %w", err)
		}
		if err := affected(res); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM session WHERE user_id=?`, id); err != nil {
			return fmt.Errorf("storage: delete sessions: %w", err)
		}
		return insertAudit(ctx, tx, e)
	})
}

// RecordLogin stores the new session, last_login, an upgraded hash and the audit entry.
func (a *authStore) RecordLogin(ctx context.Context, r LoginRecord) error {
	return a.write(ctx, []string{"user", "session", "audit_log"}, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE user SET last_login=?,
			pw_hash=COALESCE(NULLIF(?,''), pw_hash) WHERE id=?`, r.At, r.PwHash, r.UserID)
		if err != nil {
			return fmt.Errorf("storage: record login: %w", err)
		}
		s := r.Session
		_, err = tx.ExecContext(ctx, `INSERT INTO session(token_hash, user_id, created_at,
			last_seen) VALUES(?,?,?,?)`, s.TokenHash, s.UserID, s.CreatedAt, s.LastSeen)
		if err != nil {
			return fmt.Errorf("storage: create session: %w", err)
		}
		return insertAudit(ctx, tx, r.Audit)
	})
}

// Session returns the session with tokenHash or ErrNotFound.
func (a *authStore) Session(ctx context.Context, tokenHash []byte) (Session, error) {
	var s Session
	err := a.s.db.QueryRowContext(ctx, `SELECT token_hash, user_id, created_at, last_seen
		FROM session WHERE token_hash=?`, tokenHash).
		Scan(&s.TokenHash, &s.UserID, &s.CreatedAt, &s.LastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("storage: session: %w", err)
	}
	return s, nil
}

// TouchSession updates last_seen.
func (a *authStore) TouchSession(ctx context.Context, tokenHash []byte, at int64) error {
	return a.write(ctx, []string{"session"}, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE session SET last_seen=? WHERE token_hash=?`,
			at, tokenHash)
		if err != nil {
			return fmt.Errorf("storage: touch session: %w", err)
		}
		return nil
	})
}

// DeleteSession removes one session (logout).
func (a *authStore) DeleteSession(ctx context.Context, tokenHash []byte, e AuditEntry) error {
	return a.write(ctx, []string{"session", "audit_log"}, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM session WHERE token_hash=?`, tokenHash)
		if err != nil {
			return fmt.Errorf("storage: delete session: %w", err)
		}
		return insertAudit(ctx, tx, e)
	})
}

// PurgeSessions deletes idle and expired sessions.
func (a *authStore) PurgeSessions(ctx context.Context, idleBefore, createdBefore int64) error {
	return a.write(ctx, []string{"session"}, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM session WHERE last_seen<? OR created_at<?`,
			idleBefore, createdBefore)
		if err != nil {
			return fmt.Errorf("storage: purge sessions: %w", err)
		}
		return nil
	})
}

// CreateToken inserts t; a taken ID is ErrConflict.
func (a *authStore) CreateToken(ctx context.Context, t APIToken, e AuditEntry) error {
	return a.write(ctx, []string{"api_token", "audit_log"}, func(tx *sql.Tx) error {
		var n int
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_token WHERE id=?`, t.ID).Scan(&n)
		if err != nil {
			return fmt.Errorf("storage: check token: %w", err)
		}
		if n > 0 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO api_token(id, secret_hash, name, scope,
			user_id, created_at, expires_at) VALUES(?,?,?,?,NULLIF(?,0),?,NULLIF(?,0))`,
			t.ID, t.SecretHash, t.Name, t.Scope, t.UserID, t.CreatedAt, t.ExpiresAt)
		if err != nil {
			return fmt.Errorf("storage: create token: %w", err)
		}
		return insertAudit(ctx, tx, e)
	})
}

// Token returns the token with id or ErrNotFound.
func (a *authStore) Token(ctx context.Context, id string) (APIToken, error) {
	return scanToken(a.s.db.QueryRowContext(ctx,
		`SELECT `+tokenCols+` FROM api_token WHERE id=?`, id))
}

func scanToken(row scanner) (APIToken, error) {
	var t APIToken
	err := row.Scan(&t.ID, &t.SecretHash, &t.Name, &t.Scope, &t.UserID, &t.CreatedAt,
		&t.ExpiresAt, &t.LastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, ErrNotFound
	}
	if err != nil {
		return APIToken{}, fmt.Errorf("storage: scan token: %w", err)
	}
	return t, nil
}

// Tokens returns all tokens, newest first.
func (a *authStore) Tokens(ctx context.Context) ([]APIToken, error) {
	rows, err := a.s.db.QueryContext(ctx,
		`SELECT `+tokenCols+` FROM api_token ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: tokens: %w", err)
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, wrapRows(rows.Err())
}

// DeleteToken revokes a token.
func (a *authStore) DeleteToken(ctx context.Context, id string, e AuditEntry) error {
	return a.write(ctx, []string{"api_token", "audit_log"}, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM api_token WHERE id=?`, id)
		if err != nil {
			return fmt.Errorf("storage: delete token: %w", err)
		}
		if err := affected(res); err != nil {
			return err
		}
		return insertAudit(ctx, tx, e)
	})
}

// TouchToken updates last_used.
func (a *authStore) TouchToken(ctx context.Context, id string, at int64) error {
	return a.write(ctx, []string{"api_token"}, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE api_token SET last_used=? WHERE id=?`, at, id)
		if err != nil {
			return fmt.Errorf("storage: touch token: %w", err)
		}
		return nil
	})
}

// AddAudit writes one audit entry on its own (failed logins).
func (a *authStore) AddAudit(ctx context.Context, e AuditEntry) error {
	return a.write(ctx, []string{"audit_log"}, func(tx *sql.Tx) error {
		return insertAudit(ctx, tx, e)
	})
}

func insertAudit(ctx context.Context, tx *sql.Tx, e AuditEntry) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_log(at, actor, action, detail)
		VALUES(?,NULLIF(?,''),?,NULLIF(?,''))`, e.At, e.Actor, e.Action, e.Detail)
	if err != nil {
		return fmt.Errorf("storage: audit %s: %w", e.Action, err)
	}
	return nil
}

// AuditLog returns the audit entries in [from, to), newest first.
func (a *authStore) AuditLog(ctx context.Context, from, to int64, limit int,
) ([]AuditEntry, error) {
	rows, err := a.s.db.QueryContext(ctx, `SELECT id, at, COALESCE(actor,''), action,
		COALESCE(detail,'') FROM audit_log WHERE at>=? AND at<? ORDER BY at DESC, id DESC
		LIMIT ?`, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: audit log: %w", err)
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.Action, &e.Detail); err != nil {
			return nil, fmt.Errorf("storage: scan audit: %w", err)
		}
		out = append(out, e)
	}
	return out, wrapRows(rows.Err())
}
