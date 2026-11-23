package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/auth"
)

// hstsValue is sent on HTTPS responses (spec 10).
const hstsValue = "max-age=31536000"

// Authenticator resolves session cookies and API tokens.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (auth.Principal, error)
	AuthenticateToken(ctx context.Context, token string) (auth.Principal, error)
}

// Proxy resolves the client IP and HTTPS from the connection, trusting X-Forwarded-For and
// X-Forwarded-Proto only from the configured proxies (app.trusted_proxies).
func Proxy(trusted []netip.Prefix, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := auth.Client{IP: remoteIP(r), HTTPS: r.TLS != nil}
		if addr, err := netip.ParseAddr(c.IP); err == nil && isTrusted(addr, trusted) {
			if f := forwardedFor(r.Header.Get("X-Forwarded-For")); f != "" {
				c.IP = f
			}
			c.HTTPS = c.HTTPS || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
		}
		if c.HTTPS {
			w.Header().Set("Strict-Transport-Security", hstsValue)
		}
		next.ServeHTTP(w, r.WithContext(auth.WithClient(r.Context(), c)))
	})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	a = a.Unmap()
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// forwardedFor returns the last hop of X-Forwarded-For: the client as seen by the trusted
// proxy; earlier entries are client-controlled.
func forwardedFor(h string) string {
	if h == "" {
		return ""
	}
	parts := strings.Split(h, ",")
	return strings.TrimSpace(parts[len(parts)-1])
}

// Authenticate attaches the principal of a valid session cookie or API token to the
// request. A bad token is answered with 401; a stale cookie is ignored (anonymous). The
// WebSocket upgrade may carry the token as ?token= (spec 9.3).
func Authenticate(a Authenticator, log zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := principal(r, a)
		switch {
		case errors.Is(err, auth.ErrUnauthorized):
			deny(w, http.StatusUnauthorized, "unauthorized", "invalid API token", log)
			return
		case err != nil:
			log.Error().Err(err).Msg("authenticate")
			deny(w, http.StatusInternalServerError, "internal", "authentication failed", log)
			return
		case p != nil:
			r = r.WithContext(auth.WithPrincipal(r.Context(), *p))
		}
		next.ServeHTTP(w, r)
	})
}

// principal returns nil without credentials; token errors are returned, cookie errors
// other than storage failures mean anonymous.
func principal(r *http.Request, a Authenticator) (*auth.Principal, error) {
	if tok := bearer(r); tok != "" {
		p, err := a.AuthenticateToken(r.Context(), tok)
		return &p, err
	}
	c, err := r.Cookie(auth.SessionCookie)
	if err != nil || c.Value == "" {
		return nil, nil //nolint:nilnil // anonymous request
	}
	p, err := a.Authenticate(r.Context(), c.Value)
	if errors.Is(err, auth.ErrUnauthorized) {
		return nil, nil //nolint:nilnil // stale cookie: anonymous
	}
	return &p, err
}

func bearer(r *http.Request) string {
	if t, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(t)
	}
	if r.URL.Path == "/ws" {
		return r.URL.Query().Get("token")
	}
	return ""
}

// Read lets a request through when public reading is on or a principal is attached.
func Read(public bool, log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := auth.PrincipalFrom(r.Context()); !public && !ok {
				deny(w, http.StatusUnauthorized, "unauthorized", "login required", log)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Admin lets a request through only with an admin session or token (spec 10: the only
// role is admin).
func Admin(log zerolog.Logger) func(http.Handler) http.Handler { return Read(false, log) }

// CSRF rejects state-changing requests whose Origin (or Referer) is not this host
// (spec 10). Requests without either are only allowed with a bearer token, which a
// browser never adds on its own.
func CSRF(log zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safeMethod(r.Method) || sameOrigin(r) {
			next.ServeHTTP(w, r)
			return
		}
		deny(w, http.StatusForbidden, "csrf", "cross-site request rejected", log)
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func sameOrigin(r *http.Request) bool {
	src := r.Header.Get("Origin")
	if src == "" {
		src = r.Header.Get("Referer")
	}
	if src == "" {
		return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	u, err := url.Parse(src)
	return err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host)
}

// errorBody is the error shape of spec 9.2.
type errorBody struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func deny(w http.ResponseWriter, status int, code, msg string, log zerolog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	err := json.NewEncoder(w).Encode(errorBody{
		Error: http.StatusText(status), Code: code, Message: msg,
	})
	if err != nil {
		log.Debug().Err(err).Msg("write response")
	}
}
