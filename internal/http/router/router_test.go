package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoutes(t *testing.T) {
	t.Parallel()
	ok := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(name)) }
	}
	guard := func(tag string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tag))
				next.ServeHTTP(w, r)
			})
		}
	}
	mux := New(Handlers{
		Health: ok("health"), Version: ok("version"), Site: ok("site"), Energy: ok("energy"),
		Gauge: ok("gauge"), Report: ok("report"), WS: ok("ws"),
		Devices: DeviceHandlers{
			List: ok("list"), Get: ok("get"), Status: ok("status"), Overview: ok("overview"),
		},
		Auth: AuthHandlers{
			Login: ok("login"), Logout: ok("logout"), Me: ok("me"), Setup: ok("setup"),
			Users: ok("users"), AddUser: ok("adduser"), DeleteUser: ok("deluser"),
			SetPassword: ok("password"), Tokens: ok("tokens"), CreateToken: ok("addtoken"),
			RevokeToken: ok("deltoken"), AuditLog: ok("audit"),
		},
		Config: ConfigHandlers{
			Overview: ok("cfg"), Settings: ok("settings"), PutSettings: ok("putsettings"),
			Export: ok("export"), Object: ok("obj"), PutObject: ok("putobj"),
			DeleteObject: ok("delobj"), Profiles: ok("profiles"),
		},
		Docs: ok("docs"), UI: ok("ui"),
	}, Guards{Read: guard("R:"), Admin: guard("A:")})
	cases := []struct{ method, path, want string }{
		{"GET", "/health", "health"},
		{"GET", "/api/v1/version", "version"},
		{"GET", "/api/v1/site", "R:site"},
		{"GET", "/api/v1/energy", "R:energy"},
		{"GET", "/api/v1/gauge", "R:gauge"},
		{"GET", "/api/v1/report", "R:report"},
		{"GET", "/ws", "R:ws"},
		{"GET", "/api/v1/devices", "R:list"},
		{"GET", "/api/v1/devices/wr1", "R:get"},
		{"GET", "/api/v1/devices/wr1/status", "R:status"},
		{"GET", "/api/v1/status", "R:overview"},
		{"POST", "/api/v1/auth/login", "login"},
		{"POST", "/api/v1/auth/logout", "logout"},
		{"GET", "/api/v1/auth/me", "me"},
		{"POST", "/api/v1/setup", "setup"},
		{"GET", "/api/v1/users", "A:users"},
		{"POST", "/api/v1/users", "A:adduser"},
		{"DELETE", "/api/v1/users/2", "A:deluser"},
		{"PUT", "/api/v1/users/2/password", "A:password"},
		{"GET", "/api/v1/tokens", "A:tokens"},
		{"POST", "/api/v1/tokens", "A:addtoken"},
		{"DELETE", "/api/v1/tokens/abc", "A:deltoken"},
		{"GET", "/api/v1/audit", "A:audit"},
		{"GET", "/api/v1/config", "A:cfg"},
		{"GET", "/api/v1/config/settings", "A:settings"},
		{"PUT", "/api/v1/config/settings", "A:putsettings"},
		{"GET", "/api/v1/config/export", "A:export"},
		{"GET", "/api/v1/config/devices/wr1", "A:obj"},
		{"PUT", "/api/v1/config/devices/wr1", "A:putobj"},
		{"DELETE", "/api/v1/config/devices/wr1", "A:delobj"},
		{"GET", "/api/v1/profiles", "A:profiles"},
		{"GET", "/docs/", "docs"},
		{"GET", "/docs/openapi.yaml", "docs"},
		{"GET", "/", "ui"},
		{"GET", "/history", "ui"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		assert.Equal(t, c.want, rec.Body.String(), c.method+" "+c.path)
	}
	redirect := httptest.NewRecorder()
	mux.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/docs", nil))
	assert.Equal(t, "/docs/", redirect.Header().Get("Location"))
	assert.Len(t, Patterns(), len(routes(Handlers{})))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/health", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
