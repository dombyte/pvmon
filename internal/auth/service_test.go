package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"

	"github.com/dombyte/pvmon/internal/auth"
	"github.com/dombyte/pvmon/internal/database"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

const (
	testPW  = "correct horse battery"
	testPW2 = "another long passphrase"
)

var start = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

type env struct {
	svc   *auth.Service
	clock *clocktest.Clock
	store storage.AuthStore
}

func newEnv(t *testing.T, set auth.Settings) *env {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pvmon.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, database.Migrate(ctx, db, nil, start))
	s, err := storage.New(db, time.UTC)
	require.NoError(t, err)
	clock := clocktest.New(start)
	store := s.Auth()
	svc, err := auth.New(set, auth.Deps{
		Store: store, Clock: clock, Rand: rand.Reader, Log: zerolog.Nop(),
	})
	require.NoError(t, err)
	return &env{svc: svc, clock: clock, store: store}
}

func defaults() auth.Settings {
	return auth.Settings{SessionIdle: 30 * time.Minute, SessionMax: 24 * time.Hour}
}

// setup creates the first admin via the setup code.
func (e *env) setup(t *testing.T, name string) auth.Login {
	t.Helper()
	l, err := e.svc.Setup(context.Background(), auth.SetupRequest{
		Code: e.svc.SetupCode(), Name: name, Password: testPW, IP: "10.0.0.1",
	})
	require.NoError(t, err)
	return l
}

func (e *env) actions(t *testing.T) []string {
	t.Helper()
	es, err := e.store.AuditLog(context.Background(), 0, start.Add(1000*time.Hour).UnixMilli(), 100)
	require.NoError(t, err)
	out := make([]string, 0, len(es))
	for _, a := range es {
		out = append(out, a.Action)
	}
	return out
}

func sessionHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func TestNewRequiresDependencies(t *testing.T) {
	t.Parallel()
	_, err := auth.New(defaults(), auth.Deps{})
	var de *util.DependencyError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, []string{"store", "clock", "rand"}, de.Missing)
}

func TestSetup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, defaults())
	code := e.svc.SetupCode()
	require.Len(t, code, 8)
	req, err := e.svc.SetupRequired(ctx)
	require.NoError(t, err)
	assert.True(t, req)

	_, err = e.svc.Setup(ctx, auth.SetupRequest{Code: "ZZZZZZZZ", Name: "a", Password: testPW})
	require.ErrorIs(t, err, auth.ErrSetupCode)
	// right code, bad input: rejected and the code survives
	bad := []auth.SetupRequest{
		{Name: "", Password: testPW},
		{Name: "a", Password: "short"},
		{Name: "a", Password: testPW, Lang: "fr"},
	}
	for _, r := range bad {
		r.Code = code
		_, err = e.svc.Setup(ctx, r)
		require.Error(t, err)
		assert.True(t, auth.IsClientError(err), "%v", err)
	}
	assert.Equal(t, code, e.svc.SetupCode())

	shaped := strings.ToLower(code[:4] + "-" + code[4:])
	l, err := e.svc.Setup(ctx, auth.SetupRequest{
		Code: shaped, Name: " Admin ", Password: testPW, Lang: "de", IP: "10.0.0.1",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, l.Token)
	assert.Equal(t, "Admin", l.User.Name)
	assert.Equal(t, "de", l.User.Lang)
	assert.True(t, start.Add(24*time.Hour).Equal(l.Expires))
	assert.Empty(t, e.svc.SetupCode())
	req, err = e.svc.SetupRequired(ctx)
	require.NoError(t, err)
	assert.False(t, req)
	assert.Equal(t, []string{"login", "setup"}, e.actions(t))

	p, err := e.svc.Authenticate(ctx, l.Token)
	require.NoError(t, err)
	assert.Equal(t, "Admin", p.Name)

	_, err = e.svc.Setup(ctx, auth.SetupRequest{Code: code, Name: "b", Password: testPW})
	require.ErrorIs(t, err, auth.ErrSetupCode)
}

func TestSetupDoneAndRateLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, defaults())
	_, err := e.svc.AddUser(ctx, auth.CLI(), "early", testPW, "")
	require.NoError(t, err)
	_, err = e.svc.Setup(ctx, auth.SetupRequest{
		Code: e.svc.SetupCode(), Name: "b", Password: testPW,
	})
	require.ErrorIs(t, err, auth.ErrSetupDone)

	for range 5 {
		_, err = e.svc.Setup(ctx, auth.SetupRequest{Code: "0", IP: "9.9.9.9"})
		require.ErrorIs(t, err, auth.ErrSetupCode)
	}
	_, err = e.svc.Setup(ctx, auth.SetupRequest{Code: e.svc.SetupCode(), IP: "9.9.9.9"})
	var rl *auth.RateLimitError
	require.ErrorAs(t, err, &rl)
	assert.Equal(t, time.Second, rl.RetryAfter)
	assert.Contains(t, rl.Error(), "1s")
	// other addresses are not affected
	_, err = e.svc.Setup(ctx, auth.SetupRequest{Code: "0", IP: "8.8.8.8"})
	require.ErrorIs(t, err, auth.ErrSetupCode)
}

func TestLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, defaults())
	e.setup(t, "alice")

	l, err := e.svc.Login(ctx, " ALICE ", testPW, "1.1.1.1")
	require.NoError(t, err)
	assert.NotEmpty(t, l.Token)
	require.NotNil(t, l.User.LastLogin)

	_, err = e.svc.Login(ctx, "alice", "wrong password!", "1.1.1.1")
	require.ErrorIs(t, err, auth.ErrBadCredential)
	_, err = e.svc.Login(ctx, "nobody", testPW, "1.1.1.1")
	require.ErrorIs(t, err, auth.ErrBadCredential)
	acts := e.actions(t)
	assert.Equal(t, 2, strings.Count(strings.Join(acts, ","), "login_failed"))
}

func TestLoginRateLimitPerAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, defaults())
	e.setup(t, "alice")
	for i := range 5 {
		_, err := e.svc.Login(ctx, "alice", "wrong password!", fmt.Sprintf("2.2.2.%d", i))
		require.ErrorIs(t, err, auth.ErrBadCredential)
	}
	_, err := e.svc.Login(ctx, "alice", testPW, "3.3.3.3")
	var rl *auth.RateLimitError
	require.ErrorAs(t, err, &rl)
	assert.False(t, auth.IsClientError(err))

	e.clock.Advance(2 * time.Second)
	_, err = e.svc.Login(ctx, "alice", testPW, "3.3.3.3")
	require.NoError(t, err)
	// success cleared the account's failures
	_, err = e.svc.Login(ctx, "alice", "wrong password!", "3.3.3.3")
	require.ErrorIs(t, err, auth.ErrBadCredential)
}

// TestLoginParallelBurst: a burst of parallel wrong logins from one IP gets exactly the free
// attempts checked; the others wait for a slot and then see the rate limit.
func TestLoginParallelBurst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, defaults())
	e.setup(t, "alice")
	const burst = 20
	errs := make(chan error, burst)
	var wg sync.WaitGroup
	for range burst {
		wg.Go(func() {
			_, err := e.svc.Login(ctx, "alice", "wrong password!", "6.6.6.6")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	checked, limited := 0, 0
	for err := range errs {
		var rl *auth.RateLimitError
		switch {
		case errors.Is(err, auth.ErrBadCredential):
			checked++
		case errors.As(err, &rl):
			limited++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	assert.Equal(t, 5, checked)
	assert.Equal(t, burst-5, limited)
}

func TestLoginMalformedStoredHash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, defaults())
	_, err := e.store.CreateUser(ctx, storage.User{Name: "broken", PwHash: "nonsense"}, false,
		storage.AuditEntry{Action: "user_add"})
	require.NoError(t, err)
	_, err = e.svc.Login(ctx, "broken", testPW, "1.1.1.1")
	require.Error(t, err)
	assert.NotErrorIs(t, err, auth.ErrBadCredential)
}

func TestLoginUpgradesHash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, defaults())
	salt := make([]byte, 16)
	key := argon2.IDKey([]byte(testPW), salt, 1, 1024, 1, 32)
	old := fmt.Sprintf("$argon2id$v=19$m=1024,t=1,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
	_, err := e.store.CreateUser(ctx, storage.User{Name: "old", PwHash: old}, false,
		storage.AuditEntry{Action: "user_add"})
	require.NoError(t, err)
	_, err = e.svc.Login(ctx, "old", testPW, "1.1.1.1")
	require.NoError(t, err)
	u, err := e.store.UserByName(ctx, "old")
	require.NoError(t, err)
	assert.NotEqual(t, old, u.PwHash)
	assert.Contains(t, u.PwHash, "m=19456,t=2,p=1")
	_, err = e.svc.Login(ctx, "old", testPW, "1.1.1.1")
	require.NoError(t, err)
}

func TestAuthenticateAndLogout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, defaults())
	l := e.setup(t, "alice")
	h := sessionHash(l.Token)

	p, err := e.svc.Authenticate(ctx, l.Token)
	require.NoError(t, err)
	assert.Equal(t, auth.ViaSession, p.Via)
	assert.Equal(t, "user:alice", p.Actor())
	assert.Equal(t, l.User.ID, p.UserID)

	_, err = e.svc.Authenticate(ctx, "unknown-token")
	require.ErrorIs(t, err, auth.ErrUnauthorized)

	e.clock.Advance(30 * time.Second)
	_, err = e.svc.Authenticate(ctx, l.Token)
	require.NoError(t, err)
	s, err := e.store.Session(ctx, h)
	require.NoError(t, err)
	assert.Equal(t, start.UnixMilli(), s.LastSeen, "no touch within a minute")
	e.clock.Advance(40 * time.Second)
	_, err = e.svc.Authenticate(ctx, l.Token)
	require.NoError(t, err)
	s, err = e.store.Session(ctx, h)
	require.NoError(t, err)
	assert.Equal(t, e.clock.Now().UnixMilli(), s.LastSeen)

	require.ErrorIs(t, e.svc.Logout(ctx, auth.CLI()), auth.ErrUnauthorized)
	require.NoError(t, e.svc.Logout(ctx, p))
	_, err = e.svc.Authenticate(ctx, l.Token)
	require.ErrorIs(t, err, auth.ErrUnauthorized)
	assert.Contains(t, e.actions(t), "logout")
}

func TestSessionExpiry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, auth.Settings{SessionIdle: time.Hour, SessionMax: 3 * time.Hour})
	l := e.setup(t, "alice")
	e.clock.Advance(2 * time.Hour)
	_, err := e.svc.Authenticate(ctx, l.Token)
	require.ErrorIs(t, err, auth.ErrUnauthorized, "idle")

	l = func() auth.Login {
		r, err := e.svc.Login(ctx, "alice", testPW, "1.1.1.1")
		require.NoError(t, err)
		return r
	}()
	for range 5 { // stays active, but not beyond the maximum
		e.clock.Advance(50 * time.Minute)
		_, err = e.svc.Authenticate(ctx, l.Token)
		if err != nil {
			break
		}
	}
	require.ErrorIs(t, err, auth.ErrUnauthorized, "max")
}

func TestAccountManagement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, defaults())
	first := e.setup(t, "alice")
	admin, err := e.svc.Authenticate(ctx, first.Token)
	require.NoError(t, err)

	bob, err := e.svc.AddUser(ctx, admin, "bob", testPW2, "en")
	require.NoError(t, err)
	_, err = e.svc.AddUser(ctx, admin, "BOB", testPW2, "")
	require.ErrorIs(t, err, auth.ErrNameTaken)
	_, err = e.svc.AddUser(ctx, admin, "carl", "short", "")
	require.True(t, auth.IsClientError(err))

	us, err := e.svc.Users(ctx)
	require.NoError(t, err)
	require.Len(t, us, 2)
	assert.Equal(t, "alice", us[0].Name)
	u, err := e.svc.UserByName(ctx, "bob")
	require.NoError(t, err)
	assert.Equal(t, bob.ID, u.ID)
	assert.Nil(t, u.LastLogin)
	_, err = e.svc.UserByName(ctx, "nobody")
	require.ErrorIs(t, err, auth.ErrNotFound)

	bl, err := e.svc.Login(ctx, "bob", testPW2, "1.1.1.1")
	require.NoError(t, err)
	require.Error(t, e.svc.SetPassword(ctx, admin, bob.ID, "short"))
	require.ErrorIs(t, e.svc.SetPassword(ctx, admin, 999, testPW), auth.ErrNotFound)
	require.NoError(t, e.svc.SetPassword(ctx, admin, bob.ID, testPW))
	_, err = e.svc.Authenticate(ctx, bl.Token)
	require.ErrorIs(t, err, auth.ErrUnauthorized, "old sessions end")
	_, err = e.svc.Login(ctx, "bob", testPW2, "1.1.1.1")
	require.ErrorIs(t, err, auth.ErrBadCredential)
	_, err = e.svc.Login(ctx, "bob", testPW, "1.1.1.2")
	require.NoError(t, err)

	require.ErrorIs(t, e.svc.DeleteUser(ctx, admin, 999), auth.ErrNotFound)
	require.NoError(t, e.svc.DeleteUser(ctx, admin, bob.ID))
	err = e.svc.DeleteUser(ctx, admin, first.User.ID)
	require.ErrorIs(t, err, auth.ErrLastUser)
	assert.True(t, auth.IsClientError(err))
	assert.Subset(t, e.actions(t), []string{"user_add", "user_password", "user_delete"})
}

func TestTokens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, defaults())
	first := e.setup(t, "alice")
	admin, err := e.svc.Authenticate(ctx, first.Token)
	require.NoError(t, err)

	_, err = e.svc.CreateToken(ctx, admin, "ci", -time.Hour)
	require.ErrorIs(t, err, auth.ErrInvalid)
	_, err = e.svc.CreateToken(ctx, admin, " ", 0)
	require.ErrorIs(t, err, auth.ErrInvalid)

	nt, err := e.svc.CreateToken(ctx, admin, "ci", time.Hour)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(nt.Token, "pvm_"+nt.ID+"_"))
	require.NotNil(t, nt.ExpiresAt)
	assert.True(t, start.Add(time.Hour).Equal(*nt.ExpiresAt))
	forever, err := e.svc.CreateToken(ctx, auth.CLI(), "cli", 0)
	require.NoError(t, err)
	assert.Nil(t, forever.ExpiresAt)

	p, err := e.svc.AuthenticateToken(ctx, nt.Token)
	require.NoError(t, err)
	assert.Equal(t, auth.ViaToken, p.Via)
	assert.Equal(t, "token:"+nt.ID, p.Actor())
	assert.Equal(t, "ci", p.Name)
	assert.Equal(t, first.User.ID, p.UserID)
	pc, err := e.svc.AuthenticateToken(ctx, forever.Token)
	require.NoError(t, err)
	assert.Zero(t, pc.UserID)

	ts, err := e.svc.Tokens(ctx)
	require.NoError(t, err)
	require.Len(t, ts, 2)
	for _, ti := range ts {
		require.NotNil(t, ti.LastUsed, "first use is recorded")
	}
	e.clock.Advance(20 * time.Second)
	_, err = e.svc.AuthenticateToken(ctx, nt.Token)
	require.NoError(t, err)
	tok, err := e.store.Token(ctx, nt.ID)
	require.NoError(t, err)
	assert.Equal(t, start.UnixMilli(), tok.LastUsed, "no touch within a minute")
	e.clock.Advance(2 * time.Minute)
	_, err = e.svc.AuthenticateToken(ctx, nt.Token)
	require.NoError(t, err)
	tok, err = e.store.Token(ctx, nt.ID)
	require.NoError(t, err)
	assert.Equal(t, e.clock.Now().UnixMilli(), tok.LastUsed)

	badSecret := nt.Token[:len(nt.Token)-1] + "#"
	unknownID := "pvm_" + strings.Repeat("a", 8) + "_" + strings.Repeat("b", 32)
	for _, bad := range []string{badSecret, unknownID, "pvm_short", "", "Bearer x"} {
		_, err = e.svc.AuthenticateToken(ctx, bad)
		require.ErrorIs(t, err, auth.ErrUnauthorized, bad)
	}

	e.clock.Advance(2 * time.Hour)
	_, err = e.svc.AuthenticateToken(ctx, nt.Token)
	require.ErrorIs(t, err, auth.ErrUnauthorized, "expired")
	_, err = e.svc.AuthenticateToken(ctx, forever.Token)
	require.NoError(t, err)

	require.NoError(t, e.svc.RevokeToken(ctx, admin, forever.ID))
	require.ErrorIs(t, e.svc.RevokeToken(ctx, admin, forever.ID), auth.ErrNotFound)
	_, err = e.svc.AuthenticateToken(ctx, forever.Token)
	require.ErrorIs(t, err, auth.ErrUnauthorized)
	assert.Contains(t, e.actions(t), "token_revoke")
}

func TestAudit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, defaults())
	e.setup(t, "alice")
	e.clock.Advance(time.Hour)
	_, err := e.svc.Login(ctx, "ghost", testPW, "1.1.1.1")
	require.ErrorIs(t, err, auth.ErrBadCredential)

	all, err := e.svc.Audit(ctx, start, start.Add(2*time.Hour))
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, "login_failed", all[0].Action, "newest first")
	assert.JSONEq(t, `{"user":"ghost","ip":"1.1.1.1"}`, string(all[0].Detail))
	assert.Equal(t, "user:alice", all[2].Actor)
	old, err := e.svc.Audit(ctx, start, start.Add(time.Minute))
	require.NoError(t, err)
	assert.Len(t, old, 2)

	_, err = e.svc.Audit(ctx, start.Add(time.Hour), start)
	require.ErrorIs(t, err, auth.ErrInvalid)
}

func TestPrincipalAndClientContext(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "cli", auth.CLI().Actor())
	assert.Equal(t, "cli", auth.Principal{Via: "other"}.Actor())
	ctx := context.Background()
	_, ok := auth.PrincipalFrom(ctx)
	assert.False(t, ok)
	p := auth.Principal{UserID: 3, Name: "x", Via: auth.ViaSession}
	got, ok := auth.PrincipalFrom(auth.WithPrincipal(ctx, p))
	require.True(t, ok)
	assert.Equal(t, p, got)

	assert.Equal(t, auth.Client{}, auth.ClientFrom(ctx))
	c := auth.Client{IP: "1.2.3.4", HTTPS: true}
	assert.Equal(t, c, auth.ClientFrom(auth.WithClient(ctx, c)))
}

func TestIsClientError(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		&auth.PasswordError{Reason: auth.ReasonTooShort}, auth.ErrInvalid, auth.ErrNotFound,
		auth.ErrNameTaken, auth.ErrLastUser, fmt.Errorf("wrapped: %w", auth.ErrInvalid),
	} {
		assert.True(t, auth.IsClientError(err), "%v", err)
	}
	for _, err := range []error{
		nil, errors.New("boom"), auth.ErrUnauthorized, auth.ErrBadCredential,
		&auth.RateLimitError{},
	} {
		assert.False(t, auth.IsClientError(err), "%v", err)
	}
}
