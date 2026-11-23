package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/auth"
	"github.com/dombyte/pvmon/internal/util"
)

// maxBody bounds JSON request bodies.
const maxBody = 64 << 10

// AuthService is what the auth and admin handlers need.
type AuthService interface {
	SetupRequired(ctx context.Context) (bool, error)
	Setup(ctx context.Context, r auth.SetupRequest) (auth.Login, error)
	Login(ctx context.Context, name, pw, ip string) (auth.Login, error)
	Logout(ctx context.Context, p auth.Principal) error
	Users(ctx context.Context) ([]auth.UserInfo, error)
	AddUser(ctx context.Context, by auth.Principal, name, pw, lang string) (auth.UserInfo, error)
	DeleteUser(ctx context.Context, by auth.Principal, id int64) error
	SetPassword(ctx context.Context, by auth.Principal, id int64, pw string) error
	Tokens(ctx context.Context) ([]auth.TokenInfo, error)
	CreateToken(ctx context.Context, by auth.Principal, name string, expires time.Duration,
	) (auth.NewToken, error)
	RevokeToken(ctx context.Context, by auth.Principal, id string) error
	Audit(ctx context.Context, from, to time.Time) ([]auth.AuditInfo, error)
}

// Auth serves the session, setup and administration endpoints (spec 9.1).
type Auth struct {
	svc        AuthService
	publicRead bool
	clock      util.Clock
	loc        *time.Location
	log        zerolog.Logger
}

// NewAuth builds the handler; publicRead is auth.public_read, reported by /auth/me.
func NewAuth(svc AuthService, publicRead bool, clock util.Clock, loc *time.Location,
	log zerolog.Logger,
) (*Auth, error) {
	if err := util.RequireAll("handler.Auth", util.D("service", svc), util.D("clock", clock),
		util.D("location", loc)); err != nil {
		return nil, err
	}
	return &Auth{svc: svc, publicRead: publicRead, clock: clock, loc: loc, log: log}, nil
}

// loginRequest is the body of POST /api/v1/auth/login.
type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// userBody wraps one user.
type userBody struct {
	User auth.UserInfo `json:"user"`
}

// Login serves POST /api/v1/auth/login and sets the session cookie.
func (h *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !h.decode(w, r, &req) {
		return
	}
	l, err := h.svc.Login(r.Context(), req.Name, req.Password, auth.ClientFrom(r.Context()).IP)
	if err != nil {
		h.writeError(w, err)
		return
	}
	setCookie(w, r, l.Token, l.Expires, 0)
	writeJSON(w, http.StatusOK, userBody{User: l.User}, h.log)
}

// setupRequest is the body of POST /api/v1/setup.
type setupRequest struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Lang     string `json:"lang"`
}

// Setup serves POST /api/v1/setup: creates the first admin and logs them in.
func (h *Auth) Setup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if !h.decode(w, r, &req) {
		return
	}
	l, err := h.svc.Setup(r.Context(), auth.SetupRequest{
		Code: req.Code, Name: req.Name, Password: req.Password, Lang: req.Lang,
		IP: auth.ClientFrom(r.Context()).IP,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	setCookie(w, r, l.Token, l.Expires, 0)
	writeJSON(w, http.StatusCreated, userBody{User: l.User}, h.log)
}

// setCookie writes the session cookie: HttpOnly, SameSite=Strict, Secure whenever the
// request came via HTTPS, so it also works on plain HTTP in the LAN (spec 10). maxAge < 0
// deletes it.
func setCookie(w http.ResponseWriter, r *http.Request, value string, expires time.Time,
	maxAge int,
) {
	//nolint:gosec // Secure follows the request scheme (spec 10)
	http.SetCookie(w, &http.Cookie{
		Name: auth.SessionCookie, Value: value, Path: "/", Expires: expires, MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: auth.ClientFrom(r.Context()).HTTPS,
	})
}

// Logout serves POST /api/v1/auth/logout: deletes the session and the cookie.
func (h *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok || p.Via != auth.ViaSession {
		h.fail(w, apiError{http.StatusUnauthorized, "unauthorized", "no session", nil})
		return
	}
	if err := h.svc.Logout(r.Context(), p); err != nil {
		h.writeError(w, err)
		return
	}
	setCookie(w, r, "", time.Time{}, -1)
	w.WriteHeader(http.StatusNoContent)
}

// meBody is the answer of GET /api/v1/auth/me.
type meBody struct {
	User          *meUser `json:"user"`
	SetupRequired bool    `json:"setup_required"`
	PublicRead    bool    `json:"public_read"`
}

// meUser is the caller; Via is "session" or "token".
type meUser struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Via  string `json:"via"`
}

// Me serves GET /api/v1/auth/me: who is logged in, whether setup is pending and whether
// reading is public; open so the UI can decide between login, setup and dashboard.
func (h *Auth) Me(w http.ResponseWriter, r *http.Request) {
	setup, err := h.svc.SetupRequired(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	body := meBody{SetupRequired: setup, PublicRead: h.publicRead}
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		body.User = &meUser{ID: p.UserID, Name: p.Name, Via: p.Via}
	}
	writeJSON(w, http.StatusOK, body, h.log)
}

// decode reads a JSON body of at most maxBody bytes; unknown fields are rejected.
func (h *Auth) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		h.fail(w, apiError{http.StatusBadRequest, "invalid_param", "invalid JSON body", nil})
		return false
	}
	return true
}

// writeError maps auth errors onto the error codes of spec 9.2.
func (h *Auth) writeError(w http.ResponseWriter, err error) {
	var pe *auth.PasswordError
	var rl *auth.RateLimitError
	switch {
	case errors.As(err, &pe):
		h.fail(w, apiError{
			http.StatusUnprocessableEntity, "validation_failed",
			"the password does not meet the policy",
			[]errorDetail{{"password", pe.Reason}},
		})
	case errors.As(err, &rl):
		secs := int(math.Ceil(rl.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		h.fail(w, apiError{
			http.StatusTooManyRequests, "rate_limited",
			"too many failed attempts", nil,
		})
	default:
		h.fail(w, mapAuthError(err, h.log))
	}
}

// mapAuthError maps the sentinel errors; anything else is internal.
func mapAuthError(err error, log zerolog.Logger) apiError {
	for _, m := range authErrors() {
		if errors.Is(err, m.err) {
			return apiError{m.status, m.code, m.msg, nil}
		}
	}
	log.Error().Err(err).Msg("auth")
	return apiError{http.StatusInternalServerError, "internal", "the request failed", nil}
}

type authErrorMap struct {
	err    error
	status int
	code   string
	msg    string
}

func authErrors() []authErrorMap {
	return []authErrorMap{
		{
			auth.ErrBadCredential, http.StatusUnauthorized, "unauthorized",
			"invalid name or password",
		},
		{auth.ErrUnauthorized, http.StatusUnauthorized, "unauthorized", "login required"},
		{auth.ErrSetupCode, http.StatusForbidden, "forbidden", "invalid setup code"},
		{auth.ErrSetupDone, http.StatusConflict, "conflict", "setup is already done"},
		{auth.ErrNameTaken, http.StatusConflict, "conflict", "the name is already taken"},
		{auth.ErrLastUser, http.StatusConflict, "conflict", "the last user cannot be deleted"},
		{auth.ErrNotFound, http.StatusNotFound, "not_found", "not found"},
		{auth.ErrInvalid, http.StatusBadRequest, "invalid_param", "invalid input"},
	}
}

func (h *Auth) fail(w http.ResponseWriter, e apiError) { fail(w, e, h.log) }
