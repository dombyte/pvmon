package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dombyte/pvmon/internal/storage"
)

// maxAudit bounds one audit log answer.
const maxAudit = 1000

// UserInfo is a user without its password hash.
type UserInfo struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Lang      string     `json:"lang,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	LastLogin *time.Time `json:"last_login"`
}

func userInfo(u storage.User) UserInfo {
	return UserInfo{
		ID: u.ID, Name: u.Name, Lang: u.Lang, CreatedAt: time.UnixMilli(u.CreatedAt),
		LastLogin: msPtr(u.LastLogin),
	}
}

func msPtr(ms int64) *time.Time {
	if ms == 0 {
		return nil
	}
	t := time.UnixMilli(ms)
	return &t
}

// TokenInfo is an API token without its secret.
type TokenInfo struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scope     string     `json:"scope"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	LastUsed  *time.Time `json:"last_used"`
}

// NewToken is a created token; Token is shown exactly once.
type NewToken struct {
	TokenInfo
	Token string `json:"token"`
}

// AuditInfo is one audit entry.
type AuditInfo struct {
	ID     int64           `json:"id"`
	At     time.Time       `json:"at"`
	Actor  string          `json:"actor,omitempty"`
	Action string          `json:"action"`
	Detail json.RawMessage `json:"detail,omitempty"` // JSON object, never secrets
}

// Users lists all users.
func (s *Service) Users(ctx context.Context) ([]UserInfo, error) {
	us, err := s.d.Store.Users(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	out := make([]UserInfo, 0, len(us))
	for _, u := range us {
		out = append(out, userInfo(u))
	}
	return out, nil
}

// UserByName returns one user (CLI jobs).
func (s *Service) UserByName(ctx context.Context, name string) (UserInfo, error) {
	u, err := s.d.Store.UserByName(ctx, name)
	if err != nil {
		return UserInfo{}, mapStore(err)
	}
	return userInfo(u), nil
}

// AddUser creates an admin.
func (s *Service) AddUser(ctx context.Context, by Principal, name, pw, lang string,
) (UserInfo, error) {
	u, err := s.newUser(name, pw, lang)
	if err != nil {
		return UserInfo{}, err
	}
	a := s.audit(by.Actor(), ActionUserAdd, map[string]any{"user": u.Name})
	if u.ID, err = s.d.Store.CreateUser(ctx, u, false, a); err != nil {
		return UserInfo{}, mapStore(err)
	}
	return userInfo(u), nil
}

// DeleteUser deletes a user with its sessions and tokens; never the last one.
func (s *Service) DeleteUser(ctx context.Context, by Principal, id int64) error {
	u, err := s.d.Store.UserByID(ctx, id)
	if err != nil {
		return mapStore(err)
	}
	a := s.audit(by.Actor(), ActionUserDelete, map[string]any{"user": u.Name})
	return mapStore(s.d.Store.DeleteUser(ctx, id, a))
}

// SetPassword replaces a user's password and ends all of their sessions.
func (s *Service) SetPassword(ctx context.Context, by Principal, id int64, pw string) error {
	if err := checkPassword(pw, s.common); err != nil {
		return err
	}
	u, err := s.d.Store.UserByID(ctx, id)
	if err != nil {
		return mapStore(err)
	}
	hash, err := hashPassword(pw, s.d.Rand)
	if err != nil {
		return err
	}
	a := s.audit(by.Actor(), ActionUserPassword, map[string]any{"user": u.Name})
	return mapStore(s.d.Store.SetPassword(ctx, id, hash, a))
}

// Tokens lists all API tokens.
func (s *Service) Tokens(ctx context.Context) ([]TokenInfo, error) {
	ts, err := s.d.Store.Tokens(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	out := make([]TokenInfo, 0, len(ts))
	for _, t := range ts {
		out = append(out, tokenInfo(t))
	}
	return out, nil
}

func tokenInfo(t storage.APIToken) TokenInfo {
	return TokenInfo{
		ID: t.ID, Name: t.Name, Scope: t.Scope, CreatedAt: time.UnixMilli(t.CreatedAt),
		ExpiresAt: msPtr(t.ExpiresAt), LastUsed: msPtr(t.LastUsed),
	}
}

// CreateToken creates an admin token; expires 0 = never.
func (s *Service) CreateToken(ctx context.Context, by Principal, name string,
	expires time.Duration,
) (NewToken, error) {
	name, err := checkName(name)
	if err != nil {
		return NewToken{}, err
	}
	if expires < 0 {
		return NewToken{}, fmt.Errorf("%w: expiry must not be negative", ErrInvalid)
	}
	id, secret, err := newAPIToken(s.d.Rand)
	if err != nil {
		return NewToken{}, err
	}
	now := s.d.Clock.Now()
	t := storage.APIToken{
		ID: id, SecretHash: hashToken(secret), Name: name, Scope: ScopeAdmin,
		UserID: by.UserID, CreatedAt: now.UnixMilli(),
	}
	if expires > 0 {
		t.ExpiresAt = now.Add(expires).UnixMilli()
	}
	a := s.audit(by.Actor(), ActionTokenCreate, map[string]any{"token": id, "name": name})
	if err := s.d.Store.CreateToken(ctx, t, a); err != nil {
		return NewToken{}, fmt.Errorf("auth: create token: %w", err)
	}
	return NewToken{TokenInfo: tokenInfo(t), Token: tokenPrefix + id + "_" + secret}, nil
}

// RevokeToken deletes a token.
func (s *Service) RevokeToken(ctx context.Context, by Principal, id string) error {
	a := s.audit(by.Actor(), ActionTokenRevoke, map[string]any{"token": id})
	return mapStore(s.d.Store.DeleteToken(ctx, id, a))
}

// Audit returns the audit entries in [from, to), newest first (at most 1000).
func (s *Service) Audit(ctx context.Context, from, to time.Time) ([]AuditInfo, error) {
	if to.Before(from) {
		return nil, fmt.Errorf("%w: to must not be before from", ErrInvalid)
	}
	es, err := s.d.Store.AuditLog(ctx, from.UnixMilli(), to.UnixMilli(), maxAudit)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	out := make([]AuditInfo, 0, len(es))
	for _, e := range es {
		out = append(out, AuditInfo{
			ID: e.ID, At: time.UnixMilli(e.At), Actor: e.Actor, Action: e.Action,
			Detail: detailJSON(e.Detail),
		})
	}
	return out, nil
}

// detailJSON passes a stored JSON detail through; anything else becomes a JSON string.
func detailJSON(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return b
}

// IsClientError reports whether err is the caller's fault (validation, policy, not found).
func IsClientError(err error) bool {
	var pe *PasswordError
	return errors.As(err, &pe) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrNameTaken) || errors.Is(err, ErrLastUser)
}
