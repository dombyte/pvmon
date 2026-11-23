package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dombyte/pvmon/internal/storage"
)

// Audit actions.
const (
	ActionSetup        = "setup"
	ActionLogin        = "login"
	ActionLoginFailed  = "login_failed"
	ActionLogout       = "logout"
	ActionUserAdd      = "user_add"
	ActionUserDelete   = "user_delete"
	ActionUserPassword = "user_password"
	ActionTokenCreate  = "token_create"
	ActionTokenRevoke  = "token_revoke"
)

// Login is the result of a successful login or setup.
type Login struct {
	Token   string // session token for the cookie; never logged
	User    UserInfo
	Expires time.Time // absolute end of the session
}

// SetupCode returns the setup code of this process; "" once it was used.
func (s *Service) SetupCode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code
}

// SetupRequired reports whether no user exists yet.
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	n, err := s.d.Store.CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("auth: %w", err)
	}
	return n == 0, nil
}

// SetupRequest creates the first admin (POST /api/v1/setup).
type SetupRequest struct {
	Code, Name, Password, Lang, IP string
}

// Setup creates the first admin with the setup code and logs them in.
func (s *Service) Setup(ctx context.Context, r SetupRequest) (Login, error) {
	release, err := s.enter(ctx)
	if err != nil {
		return Login{}, err
	}
	defer release()
	now := s.d.Clock.Now()
	ipKey := "ip:" + r.IP
	if d := s.limit.wait(now, ipKey); d > 0 {
		return Login{}, &RateLimitError{RetryAfter: d}
	}
	if !equalCode(s.SetupCode(), r.Code) {
		s.limit.fail(now, ipKey)
		s.d.Log.Warn().Str("ip", r.IP).Msg("invalid setup code")
		return Login{}, ErrSetupCode
	}
	u, err := s.newUser(r.Name, r.Password, r.Lang)
	if err != nil {
		return Login{}, err
	}
	a := s.audit("user:"+u.Name, ActionSetup, map[string]any{"ip": r.IP})
	if u.ID, err = s.d.Store.CreateUser(ctx, u, true, a); err != nil {
		if errors.Is(err, storage.ErrConflict) {
			return Login{}, ErrSetupDone
		}
		return Login{}, fmt.Errorf("auth: setup: %w", err)
	}
	s.mu.Lock()
	s.code = ""
	s.mu.Unlock()
	s.limit.succeed(ipKey)
	s.d.Log.Info().Str("user", u.Name).Msg("first admin created")
	return s.startSession(ctx, u, "", r.IP)
}

// newUser validates the input and hashes the password.
func (s *Service) newUser(name, pw, lang string) (storage.User, error) {
	name, err := checkName(name)
	if err != nil {
		return storage.User{}, err
	}
	if err := checkLang(lang); err != nil {
		return storage.User{}, err
	}
	if err := checkPassword(pw, s.common); err != nil {
		return storage.User{}, err
	}
	hash, err := hashPassword(pw, s.d.Rand)
	if err != nil {
		return storage.User{}, err
	}
	return storage.User{
		Name: name, PwHash: hash, Lang: lang, CreatedAt: s.d.Clock.Now().UnixMilli(),
	}, nil
}

// Login checks name and password. Unknown names and wrong passwords answer the same way
// and take the same time (spec 10).
func (s *Service) Login(ctx context.Context, name, pw, ip string) (Login, error) {
	release, err := s.enter(ctx)
	if err != nil {
		return Login{}, err
	}
	defer release()
	now := s.d.Clock.Now()
	keys := []string{"ip:" + ip, "user:" + strings.ToLower(strings.TrimSpace(name))}
	if d := s.limit.wait(now, keys...); d > 0 {
		return Login{}, &RateLimitError{RetryAfter: d}
	}
	u, upgrade, err := s.check(ctx, strings.TrimSpace(name), pw)
	if errors.Is(err, ErrBadCredential) {
		s.limit.fail(now, keys...)
		s.d.Log.Warn().Str("user", name).Str("ip", ip).Msg("login failed")
		a := s.audit("", ActionLoginFailed, map[string]any{"user": name, "ip": ip})
		return Login{}, errors.Join(err, s.d.Store.AddAudit(ctx, a))
	}
	if err != nil {
		return Login{}, err
	}
	s.limit.succeed(keys...)
	hash := ""
	if upgrade {
		if hash, err = hashPassword(pw, s.d.Rand); err != nil {
			return Login{}, err
		}
	}
	return s.startSession(ctx, u, hash, ip)
}

// check verifies the password; upgrade reports outdated hash parameters.
// enter runs Login and Setup one at a time: each Argon2id hash takes 19 MiB, and the
// limiter is asked only after the attempts ahead of this one were counted.
func (s *Service) enter(ctx context.Context) (func(), error) {
	select {
	case s.gate <- struct{}{}:
		return func() { <-s.gate }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("auth: wait for a login slot: %w", ctx.Err())
	}
}

func (s *Service) check(ctx context.Context, name, pw string) (storage.User, bool, error) {
	u, err := s.d.Store.UserByName(ctx, name)
	if errors.Is(err, storage.ErrNotFound) {
		s.burn(pw)
		return storage.User{}, false, ErrBadCredential
	}
	if err != nil {
		return storage.User{}, false, fmt.Errorf("auth: %w", err)
	}
	ok, upgrade, err := verifyPassword(pw, u.PwHash)
	if err != nil {
		return storage.User{}, false, fmt.Errorf("auth: user %d: %w", u.ID, err)
	}
	if !ok {
		return storage.User{}, false, ErrBadCredential
	}
	return u, upgrade, nil
}

// startSession stores a new session; hash replaces the stored password hash when set.
func (s *Service) startSession(ctx context.Context, u storage.User, hash, ip string,
) (Login, error) {
	now := s.d.Clock.Now()
	tok, err := newSessionToken(s.d.Rand)
	if err != nil {
		return Login{}, err
	}
	ms := now.UnixMilli()
	err = s.d.Store.RecordLogin(ctx, storage.LoginRecord{
		UserID: u.ID, At: ms, PwHash: hash,
		Session: storage.Session{
			TokenHash: hashToken(tok), UserID: u.ID, CreatedAt: ms, LastSeen: ms,
		},
		Audit: s.audit("user:"+u.Name, ActionLogin, map[string]any{"ip": ip}),
	})
	if err != nil {
		return Login{}, fmt.Errorf("auth: login: %w", err)
	}
	u.LastLogin = ms
	s.purge(ctx, now)
	return Login{Token: tok, User: userInfo(u), Expires: now.Add(s.s.SessionMax)}, nil
}

// purge drops expired sessions; a failure only delays the cleanup to the next login.
func (s *Service) purge(ctx context.Context, now time.Time) {
	err := s.d.Store.PurgeSessions(ctx, now.Add(-s.s.SessionIdle).UnixMilli(),
		now.Add(-s.s.SessionMax).UnixMilli())
	if err != nil {
		s.d.Log.Warn().Err(err).Msg("purge sessions")
	}
}

// Logout deletes the session of p server-side.
func (s *Service) Logout(ctx context.Context, p Principal) error {
	if p.Via != ViaSession {
		return ErrUnauthorized
	}
	a := s.audit(p.Actor(), ActionLogout, nil)
	if err := s.d.Store.DeleteSession(ctx, p.session, a); err != nil {
		return fmt.Errorf("auth: logout: %w", err)
	}
	return nil
}

// burn verifies pw against a dummy hash so an unknown name takes as long as a wrong
// password (spec 10); the result is irrelevant.
func (s *Service) burn(pw string) {
	if _, _, err := verifyPassword(pw, s.dummy); err != nil {
		s.d.Log.Error().Err(err).Msg("dummy hash")
	}
}

// Authenticate resolves a session token; expired or unknown sessions are ErrUnauthorized.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	h := hashToken(token)
	sess, err := s.d.Store.Session(ctx, h)
	if err != nil {
		return Principal{}, unauthorized(err)
	}
	now := s.d.Clock.Now()
	if !s.sessionValid(sess, now) {
		return Principal{}, ErrUnauthorized
	}
	u, err := s.d.Store.UserByID(ctx, sess.UserID)
	if err != nil {
		return Principal{}, unauthorized(err)
	}
	if now.Sub(time.UnixMilli(sess.LastSeen)) >= touchEvery {
		if err := s.d.Store.TouchSession(ctx, h, now.UnixMilli()); err != nil {
			s.d.Log.Warn().Err(err).Msg("touch session")
		}
	}
	return Principal{UserID: u.ID, Name: u.Name, Via: ViaSession, session: h}, nil
}

// unauthorized maps a missing row to ErrUnauthorized and wraps other errors.
func unauthorized(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return ErrUnauthorized
	}
	return fmt.Errorf("auth: %w", err)
}

func (s *Service) sessionValid(sess storage.Session, now time.Time) bool {
	return now.Sub(time.UnixMilli(sess.LastSeen)) <= s.s.SessionIdle &&
		now.Sub(time.UnixMilli(sess.CreatedAt)) <= s.s.SessionMax
}

// AuthenticateToken resolves an API token pvm_<id>_<secret>.
func (s *Service) AuthenticateToken(ctx context.Context, token string) (Principal, error) {
	id, secret, ok := splitAPIToken(token)
	if !ok {
		return Principal{}, ErrUnauthorized
	}
	t, err := s.d.Store.Token(ctx, id)
	if err != nil {
		return Principal{}, unauthorized(err)
	}
	now := s.d.Clock.Now()
	if !tokenValid(t, secret, now) {
		return Principal{}, ErrUnauthorized
	}
	if now.Sub(time.UnixMilli(t.LastUsed)) >= touchEvery {
		if err := s.d.Store.TouchToken(ctx, id, now.UnixMilli()); err != nil {
			s.d.Log.Warn().Err(err).Msg("touch token")
		}
	}
	return Principal{UserID: t.UserID, Name: t.Name, Via: ViaToken, TokenID: t.ID}, nil
}

func tokenValid(t storage.APIToken, secret string, now time.Time) bool {
	return subtle.ConstantTimeCompare(hashToken(secret), t.SecretHash) == 1 &&
		t.Scope == ScopeAdmin && (t.ExpiresAt == 0 || now.UnixMilli() < t.ExpiresAt)
}
