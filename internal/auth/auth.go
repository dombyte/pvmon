// Package auth implements accounts, sessions, API tokens, the setup code, brute-force
// limits and the audit entries of these actions (spec 10). It owns the write domain auth
// (spec 7.5) and talks to storage only through storage.AuthStore.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
)

// Errors for branching in handlers and CLI jobs.
var (
	ErrUnauthorized  = errors.New("auth: unauthorized")
	ErrBadCredential = errors.New("auth: invalid name or password")
	ErrSetupCode     = errors.New("auth: invalid setup code")
	ErrSetupDone     = errors.New("auth: setup already done")
	ErrNameTaken     = errors.New("auth: name already taken")
	ErrInvalid       = errors.New("auth: invalid input")
	ErrNotFound      = errors.New("auth: not found")
	ErrLastUser      = errors.New("auth: the last user cannot be deleted")
)

// RateLimitError means too many failed attempts; retry after RetryAfter.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("auth: too many failed attempts, retry in %s", e.RetryAfter)
}

// Scope of API tokens (spec 10; "control" is reserved).
const ScopeAdmin = "admin"

// Ways a principal authenticated.
const (
	ViaSession = "session"
	ViaToken   = "token"
	ViaCLI     = "cli"
)

// touchEvery bounds last_seen/last_used writes to one per minute and credential.
const touchEvery = time.Minute

// maxNameLen bounds user and token names.
const maxNameLen = 64

// Settings are the session lifetimes (auth.session_idle, auth.session_max).
type Settings struct {
	SessionIdle time.Duration
	SessionMax  time.Duration
}

// Deps are the dependencies of the service.
type Deps struct {
	Store storage.AuthStore
	Clock util.Clock
	Rand  io.Reader // crypto/rand.Reader in production
	Log   zerolog.Logger
}

// Service implements authentication and account management.
type Service struct {
	s      Settings
	d      Deps
	common map[string]bool
	limit  *limiter
	dummy  string        // hash verified for unknown users so timing does not reveal names
	gate   chan struct{} // one Login or Setup at a time (see enter)

	mu   sync.Mutex
	code string // setup code; "" once used
}

// New builds the service and draws the setup code of this process (spec 10).
func New(s Settings, d Deps) (*Service, error) {
	if err := util.RequireAll("auth", util.D("store", d.Store), util.D("clock", d.Clock),
		util.D("rand", d.Rand)); err != nil {
		return nil, err
	}
	code, err := newSetupCode(d.Rand)
	if err != nil {
		return nil, err
	}
	dummy, err := hashPassword(code, d.Rand)
	if err != nil {
		return nil, err
	}
	return &Service{
		s: s, d: d, common: commonPasswords(), limit: newLimiter(), dummy: dummy, code: code,
		gate: make(chan struct{}, 1),
	}, nil
}

// Principal is an authenticated caller.
type Principal struct {
	UserID  int64
	Name    string // user name, or token name for tokens
	Via     string // ViaSession, ViaToken or ViaCLI
	TokenID string
	session []byte
}

// CLI is the principal of CLI jobs.
func CLI() Principal { return Principal{Name: "cli", Via: ViaCLI} }

// Actor is the audit actor of p: "user:<name>", "token:<id>" or "cli".
func (p Principal) Actor() string {
	switch p.Via {
	case ViaToken:
		return "token:" + p.TokenID
	case ViaSession:
		return "user:" + p.Name
	}
	return ViaCLI
}

type principalKey struct{}

// WithPrincipal returns ctx carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal of ctx; ok is false for anonymous requests.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// audit builds an audit entry; detail is marshalled to JSON and never holds secrets.
func (s *Service) audit(actor, action string, detail map[string]any) storage.AuditEntry {
	e := storage.AuditEntry{At: s.d.Clock.Now().UnixMilli(), Actor: actor, Action: action}
	if len(detail) > 0 {
		b, err := json.Marshal(detail)
		if err == nil {
			e.Detail = string(b)
		}
	}
	return e
}

// checkName validates a user or token name.
func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxNameLen || strings.ContainsFunc(name, isControl) {
		return "", fmt.Errorf("%w: name must be 1..%d printable characters", ErrInvalid,
			maxNameLen)
	}
	return name, nil
}

func isControl(r rune) bool { return r < ' ' || r == 0x7f }

// checkLang accepts the UI languages (spec 11.10) or none.
func checkLang(lang string) error {
	if lang != "" && lang != "de" && lang != "en" {
		return fmt.Errorf("%w: lang must be de or en", ErrInvalid)
	}
	return nil
}

// mapStore translates storage sentinels into auth sentinels.
func mapStore(err error) error {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, storage.ErrConflict):
		return ErrNameTaken
	case errors.Is(err, storage.ErrLastUser):
		return ErrLastUser
	}
	return err
}
