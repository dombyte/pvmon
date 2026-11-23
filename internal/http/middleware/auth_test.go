package middleware_test

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/auth"
	"github.com/dombyte/pvmon/internal/http/middleware"
	"github.com/dombyte/pvmon/internal/http/middleware/mocks"
)

func TestProxy(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.1.5/32"),
	}
	tests := []struct {
		name      string
		remote    string
		tls       bool
		xff       string
		proto     string
		wantIP    string
		wantHTTPS bool
	}{
		{
			"untrusted ignores headers", "203.0.113.9:1234", false, "1.1.1.1", "https",
			"203.0.113.9", false,
		},
		{
			"trusted prefix last entry", "10.1.2.3:80", false, "9.9.9.9, 1.1.1.1 ", "HTTPS",
			"1.1.1.1", true,
		},
		{"trusted single ip", "192.168.1.5:80", false, "2.2.2.2", "http", "2.2.2.2", false},
		{
			"ipv4-mapped remote", "[::ffff:10.0.0.1]:80", false, "3.3.3.3", "https",
			"3.3.3.3", true,
		},
		{"trusted without headers", "10.0.0.1:80", false, "", "", "10.0.0.1", false},
		{"direct tls", "203.0.113.9:443", true, "", "", "203.0.113.9", true},
		{"no port", "10.0.0.1", false, "4.4.4.4", "", "4.4.4.4", false},
		{"untrusted no port", "203.0.113.9", false, "", "", "203.0.113.9", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got auth.Client
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = auth.ClientFrom(r.Context())
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remote
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.proto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			rec := httptest.NewRecorder()
			middleware.Proxy(trusted, next).ServeHTTP(rec, req)
			assert.Equal(t, tc.wantIP, got.IP)
			assert.Equal(t, tc.wantHTTPS, got.HTTPS)
			if tc.wantHTTPS {
				assert.NotEmpty(t, rec.Header().Get("Strict-Transport-Security"))
			} else {
				assert.Empty(t, rec.Header().Get("Strict-Transport-Security"))
			}
		})
	}
}

// serve runs the Authenticate middleware and reports the status and attached principal.
func serve(
	a middleware.Authenticator, req *http.Request,
) (*httptest.ResponseRecorder, *auth.Principal) {
	var got *auth.Principal
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if p, ok := auth.PrincipalFrom(r.Context()); ok {
			got = &p
		}
	})
	rec := httptest.NewRecorder()
	middleware.Authenticate(a, zerolog.Nop(), next).ServeHTTP(rec, req)
	return rec, got
}

func cookieReq(path, value string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: value})
	return req
}

func TestAuthenticateAnonymous(t *testing.T) {
	t.Parallel()
	a := mocks.NewMockAuthenticator(t)
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/", nil),
		cookieReq("/", ""),
		httptest.NewRequest(http.MethodGet, "/api?token=abc", nil),
	} {
		rec, p := serve(a, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Nil(t, p)
	}
}

func TestAuthenticateCookie(t *testing.T) {
	t.Parallel()
	want := auth.Principal{UserID: 3, Name: "ann", Via: auth.ViaSession}
	tests := []struct {
		name     string
		ret      auth.Principal
		err      error
		wantCode int
		wantP    bool
		wantBody string
	}{
		{"valid", want, nil, http.StatusOK, true, ""},
		{"stale", auth.Principal{}, auth.ErrUnauthorized, http.StatusOK, false, ""},
		{
			"storage error",
			auth.Principal{},
			errors.New("db down"),
			http.StatusInternalServerError, false, `"code":"internal"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := mocks.NewMockAuthenticator(t)
			a.EXPECT().Authenticate(mock.Anything, "sess").Return(tc.ret, tc.err)
			rec, p := serve(a, cookieReq("/", "sess"))
			assert.Equal(t, tc.wantCode, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.wantBody)
			if tc.wantP {
				require.NotNil(t, p)
				assert.Equal(t, want, *p)
			} else {
				assert.Nil(t, p)
			}
		})
	}
}

func TestAuthenticateBearer(t *testing.T) {
	t.Parallel()
	want := auth.Principal{Name: "tok", Via: auth.ViaToken, TokenID: "t1"}
	a := mocks.NewMockAuthenticator(t)
	a.EXPECT().AuthenticateToken(mock.Anything, "good").Return(want, nil)
	a.EXPECT().AuthenticateToken(mock.Anything, "bad").
		Return(auth.Principal{}, auth.ErrUnauthorized)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer good")
	rec, p := serve(a, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, p)
	assert.Equal(t, want, *p)

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer bad")
	rec, p = serve(a, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"unauthorized"`)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Nil(t, p)
}

func TestAuthenticateWSToken(t *testing.T) {
	t.Parallel()
	a := mocks.NewMockAuthenticator(t)
	a.EXPECT().AuthenticateToken(mock.Anything, "wstok").
		Return(auth.Principal{Via: auth.ViaToken}, nil)
	rec, p := serve(a, httptest.NewRequest(http.MethodGet, "/ws?token=wstok", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotNil(t, p)
}

func TestReadAdmin(t *testing.T) {
	t.Parallel()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	withP := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		return req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{Name: "a"}))
	}
	anon := func() *http.Request { return httptest.NewRequest(http.MethodGet, "/", nil) }
	log := zerolog.Nop()
	tests := []struct {
		name string
		mw   func(http.Handler) http.Handler
		req  *http.Request
		want int
	}{
		{"public anonymous", middleware.Read(true, log), anon(), http.StatusNoContent},
		{"private anonymous", middleware.Read(false, log), anon(), http.StatusUnauthorized},
		{"private principal", middleware.Read(false, log), withP(), http.StatusNoContent},
		{"admin anonymous", middleware.Admin(log), anon(), http.StatusUnauthorized},
		{"admin principal", middleware.Admin(log), withP(), http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			tc.mw(ok).ServeHTTP(rec, tc.req)
			assert.Equal(t, tc.want, rec.Code)
		})
	}
}

func TestCSRF(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{"get", http.MethodGet, nil, http.StatusNoContent},
		{"head", http.MethodHead, nil, http.StatusNoContent},
		{"options", http.MethodOptions, nil, http.StatusNoContent},
		{
			"origin match", http.MethodPost,
			map[string]string{"Origin": "http://pv.lan"},
			http.StatusNoContent,
		},
		{
			"origin case", http.MethodPost,
			map[string]string{"Origin": "http://PV.LAN"},
			http.StatusNoContent,
		},
		{
			"origin mismatch", http.MethodPost,
			map[string]string{"Origin": "http://evil.example"},
			http.StatusForbidden,
		},
		{
			"referer match", http.MethodDelete,
			map[string]string{"Referer": "http://pv.lan/page"},
			http.StatusNoContent,
		},
		{
			"referer mismatch", http.MethodPut,
			map[string]string{"Referer": "http://evil.example/"},
			http.StatusForbidden,
		},
		{"origin wins over referer", http.MethodPost, map[string]string{
			"Origin": "http://evil.example", "Referer": "http://pv.lan/",
		}, http.StatusForbidden},
		{
			"unparsable origin", http.MethodPost,
			map[string]string{"Origin": "://"},
			http.StatusForbidden,
		},
		{
			"origin without host", http.MethodPost,
			map[string]string{"Origin": "null"},
			http.StatusForbidden,
		},
		{"none", http.MethodPost, nil, http.StatusForbidden},
		{
			"bearer", http.MethodPost,
			map[string]string{"Authorization": "Bearer x"},
			http.StatusNoContent,
		},
		{
			"basic auth", http.MethodPost,
			map[string]string{"Authorization": "Basic x"},
			http.StatusForbidden,
		},
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, "/x", nil)
			req.Host = "pv.lan"
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			middleware.CSRF(zerolog.Nop(), next).ServeHTTP(rec, req)
			assert.Equal(t, tc.want, rec.Code)
			if tc.want == http.StatusForbidden {
				assert.Contains(t, rec.Body.String(), `"code":"csrf"`)
			}
		})
	}
}
