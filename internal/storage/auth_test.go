package storage

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func audit(action string, at int64) AuditEntry {
	return AuditEntry{At: at, Actor: "cli", Action: action}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
	return n
}

func mustUser(t *testing.T, a AuthStore, name string) int64 {
	t.Helper()
	id, err := a.CreateUser(context.Background(),
		User{Name: name, PwHash: "h-" + name, Lang: "de", CreatedAt: 10}, false,
		audit("user_add", 10))
	require.NoError(t, err)
	return id
}

func TestAuthUsers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	a := s.Auth()

	n, err := a.CountUsers(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)

	id, err := a.CreateUser(ctx, User{Name: "Alice", PwHash: "h", CreatedAt: 5}, true,
		audit("setup", 5))
	require.NoError(t, err)
	_, err = a.CreateUser(ctx, User{Name: "Bob", PwHash: "h"}, true, audit("setup", 6))
	require.ErrorIs(t, err, ErrConflict)
	_, err = a.CreateUser(ctx, User{Name: "alice", PwHash: "h"}, false, audit("user_add", 6))
	require.ErrorIs(t, err, ErrConflict, "names are case-insensitive")
	assert.Equal(t, 1, countRows(t, db, "audit_log"), "failed writes roll back their audit")

	bob := mustUser(t, a, "bob")
	u, err := a.UserByName(ctx, "ALICE")
	require.NoError(t, err)
	assert.Equal(t, id, u.ID)
	assert.Equal(t, "Alice", u.Name)
	assert.Empty(t, u.Lang)
	assert.Zero(t, u.LastLogin)
	u, err = a.UserByID(ctx, bob)
	require.NoError(t, err)
	assert.Equal(t, "de", u.Lang)
	_, err = a.UserByName(ctx, "nobody")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = a.UserByID(ctx, 999)
	require.ErrorIs(t, err, ErrNotFound)

	us, err := a.Users(ctx)
	require.NoError(t, err)
	require.Len(t, us, 2)
	assert.Equal(t, "Alice", us[0].Name)
	assert.Equal(t, "bob", us[1].Name)
}

func TestAuthDeleteUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	a := s.Auth()
	one := mustUser(t, a, "one")
	require.ErrorIs(t, a.DeleteUser(ctx, one, audit("user_delete", 20)), ErrLastUser)
	assert.Equal(t, 1, countRows(t, db, "user"))
	assert.Equal(t, 1, countRows(t, db, "audit_log"))

	two := mustUser(t, a, "two")
	require.ErrorIs(t, a.DeleteUser(ctx, 999, audit("user_delete", 20)), ErrNotFound)
	require.NoError(t, a.RecordLogin(ctx, LoginRecord{
		UserID: two, At: 30,
		Session: Session{TokenHash: []byte("s1"), UserID: two, CreatedAt: 30, LastSeen: 30},
		Audit:   audit("login", 30),
	}))
	require.NoError(t, a.CreateToken(ctx,
		APIToken{ID: "t1", SecretHash: []byte("x"), Name: "n", Scope: "admin", UserID: two},
		audit("token_create", 31)))
	before := countRows(t, db, "audit_log")
	require.NoError(t, a.DeleteUser(ctx, two, audit("user_delete", 40)))
	assert.Equal(t, before+1, countRows(t, db, "audit_log"))
	assert.Zero(t, countRows(t, db, "session"))
	assert.Zero(t, countRows(t, db, "api_token"))
}

func TestAuthPasswordAndLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	a := s.Auth()
	id := mustUser(t, a, "alice")
	login := func(hash string, hs string, at int64) {
		require.NoError(t, a.RecordLogin(ctx, LoginRecord{
			UserID: id, At: at, PwHash: hash,
			Session: Session{TokenHash: []byte(hs), UserID: id, CreatedAt: at, LastSeen: at},
			Audit:   audit("login", at),
		}))
	}
	login("", "s1", 100)
	u, err := a.UserByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "h-alice", u.PwHash, "empty hash keeps the stored one")
	assert.EqualValues(t, 100, u.LastLogin)
	login("upgraded", "s2", 200)
	u, err = a.UserByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "upgraded", u.PwHash)
	assert.EqualValues(t, 200, u.LastLogin)
	assert.Equal(t, 2, countRows(t, db, "session"))

	before := countRows(t, db, "audit_log")
	require.NoError(t, a.SetPassword(ctx, id, "new", audit("user_password", 300)))
	assert.Zero(t, countRows(t, db, "session"))
	assert.Equal(t, before+1, countRows(t, db, "audit_log"))
	u, err = a.UserByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "new", u.PwHash)
	require.ErrorIs(t, a.SetPassword(ctx, 999, "x", audit("user_password", 1)), ErrNotFound)
}

func TestAuthSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	a := s.Auth()
	id := mustUser(t, a, "alice")
	for i, h := range []string{"old", "idle", "fresh"} {
		at := int64(100 * (i + 1))
		require.NoError(t, a.RecordLogin(ctx, LoginRecord{
			UserID: id, At: at,
			Session: Session{TokenHash: []byte(h), UserID: id, CreatedAt: at, LastSeen: at},
			Audit:   audit("login", at),
		}))
	}
	got, err := a.Session(ctx, []byte("fresh"))
	require.NoError(t, err)
	assert.Equal(t, Session{
		TokenHash: []byte("fresh"), UserID: id, CreatedAt: 300,
		LastSeen: 300,
	}, got)
	_, err = a.Session(ctx, []byte("none"))
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, a.TouchSession(ctx, []byte("old"), 1000))
	require.NoError(t, a.TouchSession(ctx, []byte("fresh"), 1000))
	got, err = a.Session(ctx, []byte("old"))
	require.NoError(t, err)
	assert.EqualValues(t, 1000, got.LastSeen)

	// idle: last_seen < 500 removes "idle"; created < 150 removes "old"
	require.NoError(t, a.PurgeSessions(ctx, 500, 150))
	assert.Equal(t, 1, countRows(t, db, "session"))
	before := countRows(t, db, "audit_log")
	require.NoError(t, a.DeleteSession(ctx, []byte("fresh"), audit("logout", 2000)))
	assert.Zero(t, countRows(t, db, "session"))
	assert.Equal(t, before+1, countRows(t, db, "audit_log"))
}

func TestAuthTokens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	a := s.Auth()
	id := mustUser(t, a, "alice")
	require.NoError(t, a.CreateToken(ctx, APIToken{
		ID: "cli00001", SecretHash: []byte("h1"),
		Name: "from cli", Scope: "admin", CreatedAt: 10,
	}, audit("token_create", 10)))
	require.NoError(t, a.CreateToken(ctx, APIToken{
		ID: "usr00001", SecretHash: []byte("h2"),
		Name: "from user", Scope: "admin", UserID: id, CreatedAt: 20, ExpiresAt: 99,
	},
		audit("token_create", 20)))
	err := a.CreateToken(ctx, APIToken{ID: "cli00001", Name: "dup", Scope: "admin"},
		audit("token_create", 30))
	require.ErrorIs(t, err, ErrConflict)

	var nulls int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM api_token WHERE user_id IS NULL AND expires_at IS NULL`).Scan(&nulls))
	assert.Equal(t, 1, nulls)

	tok, err := a.Token(ctx, "cli00001")
	require.NoError(t, err)
	assert.Zero(t, tok.UserID)
	assert.Zero(t, tok.ExpiresAt)
	assert.Equal(t, []byte("h1"), tok.SecretHash)
	_, err = a.Token(ctx, "nope")
	require.ErrorIs(t, err, ErrNotFound)

	ts, err := a.Tokens(ctx)
	require.NoError(t, err)
	require.Len(t, ts, 2)
	assert.Equal(t, "usr00001", ts[0].ID, "newest first")
	assert.Equal(t, id, ts[0].UserID)
	assert.EqualValues(t, 99, ts[0].ExpiresAt)

	require.NoError(t, a.TouchToken(ctx, "usr00001", 555))
	tok, err = a.Token(ctx, "usr00001")
	require.NoError(t, err)
	assert.EqualValues(t, 555, tok.LastUsed)

	before := countRows(t, db, "audit_log")
	require.NoError(t, a.DeleteToken(ctx, "usr00001", audit("token_revoke", 40)))
	assert.Equal(t, before+1, countRows(t, db, "audit_log"))
	require.ErrorIs(t, a.DeleteToken(ctx, "usr00001", audit("token_revoke", 41)), ErrNotFound)
	assert.Equal(t, before+1, countRows(t, db, "audit_log"))
}

func TestAuthAuditLog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStorage(t)
	a := s.Auth()
	for _, at := range []int64{10, 20, 30, 40} {
		e := AuditEntry{At: at, Action: "login_failed"}
		if at == 20 {
			e.Actor, e.Detail = "user:x", `{"ip":"1.2.3.4"}`
		}
		require.NoError(t, a.AddAudit(ctx, e))
	}
	es, err := a.AuditLog(ctx, 20, 40, 10)
	require.NoError(t, err)
	require.Len(t, es, 2)
	assert.EqualValues(t, 30, es[0].At, "to is exclusive, newest first")
	assert.Equal(t, "user:x", es[1].Actor)
	assert.JSONEq(t, `{"ip":"1.2.3.4"}`, es[1].Detail)
	assert.Empty(t, es[0].Actor)
	es, err = a.AuditLog(ctx, 0, 100, 3)
	require.NoError(t, err)
	require.Len(t, es, 3)
	assert.EqualValues(t, 40, es[0].At)
}

func TestAuthDomainGuard(t *testing.T) {
	t.Parallel()
	err := (&Storage{}).write(context.Background(), DomainAuth, []string{"b15_energy"},
		func(*sql.Tx) error { return nil })
	var de *DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, "b15_energy", de.Table)
}

func TestAuthClosedDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	a := s.Auth()
	require.NoError(t, db.Close())
	e := audit("x", 1)
	h := []byte("h")
	calls := map[string]func() error{
		"count":    func() error { _, err := a.CountUsers(ctx); return err },
		"create":   func() error { _, err := a.CreateUser(ctx, User{Name: "a"}, false, e); return err },
		"byname":   func() error { _, err := a.UserByName(ctx, "a"); return err },
		"users":    func() error { _, err := a.Users(ctx); return err },
		"delete":   func() error { return a.DeleteUser(ctx, 1, e) },
		"password": func() error { return a.SetPassword(ctx, 1, "h", e) },
		"login":    func() error { return a.RecordLogin(ctx, LoginRecord{UserID: 1}) },
		"session":  func() error { _, err := a.Session(ctx, h); return err },
		"touch":    func() error { return a.TouchSession(ctx, h, 1) },
		"delsess":  func() error { return a.DeleteSession(ctx, h, e) },
		"purge":    func() error { return a.PurgeSessions(ctx, 1, 1) },
		"mktoken":  func() error { return a.CreateToken(ctx, APIToken{ID: "a"}, e) },
		"token":    func() error { _, err := a.Token(ctx, "a"); return err },
		"tokens":   func() error { _, err := a.Tokens(ctx); return err },
		"deltoken": func() error { return a.DeleteToken(ctx, "a", e) },
		"touchtok": func() error { return a.TouchToken(ctx, "a", 1) },
		"audit":    func() error { return a.AddAudit(ctx, e) },
		"auditlog": func() error { _, err := a.AuditLog(ctx, 0, 1, 1); return err },
	}
	for name, f := range calls {
		err := f()
		require.Error(t, err, name)
		assert.NotErrorIs(t, err, ErrNotFound, name)
	}
}
