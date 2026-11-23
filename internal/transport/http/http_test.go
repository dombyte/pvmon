package http

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/util/clocktest"
)

var t0 = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func newClient(t *testing.T, s Settings) (*Client, *clocktest.Clock) {
	t.Helper()
	clock := clocktest.New(t0)
	if s.Timeout == 0 {
		s.Timeout = time.Second
	}
	c, err := New(s, clock, zerolog.Nop())
	require.NoError(t, err)
	return c, clock
}

// digestServer verifies digest auth with the given algorithm, then answers {"ok":true}.
func digestServer(t *testing.T, algorithm string, newHash func() hash.Hash) *httptest.Server {
	t.Helper()
	h := func(s string) string {
		x := newHash()
		x.Write([]byte(s))
		return hex.EncodeToString(x.Sum(nil))
	}
	return httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		p := parseAuth(r.Header.Get("Authorization"))
		ha1 := h("admin:shelly:secret")
		want := h(strings.Join([]string{
			ha1, "abc", p["nc"], p["cnonce"], "auth",
			h("GET:" + r.URL.RequestURI()),
		}, ":"))
		if p["response"] != want || p["opaque"] != "op" {
			w.Header().Set("WWW-Authenticate", `Digest qop="auth,auth-int", realm="shelly", `+
				`nonce="abc", opaque="op", algorithm=`+algorithm)
			w.WriteHeader(nethttp.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
}

func parseAuth(header string) map[string]string {
	out := map[string]string{}
	rest, _ := strings.CutPrefix(header, "Digest ")
	for _, part := range splitParams(rest) {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		out[k] = strings.Trim(v, `"`)
	}
	return out
}

func TestDigest(t *testing.T) {
	t.Parallel()
	for alg, hf := range map[string]func() hash.Hash{"MD5": md5.New, "SHA-256": sha256.New} {
		srv := digestServer(t, alg, hf)
		defer srv.Close()
		c, _ := newClient(t, Settings{
			BaseURL: srv.URL, Auth: AuthDigest, User: "admin",
			Password: "secret",
		})
		for range 3 { // first request learns the challenge, later ones reuse it
			body, err := c.Get(context.Background(), "/rpc/EM.GetStatus?id=0")
			require.NoError(t, err, alg)
			assert.JSONEq(t, `{"ok":true}`, string(body))
		}
		assert.Equal(t, 3, c.digest.nc, "challenge reused with increasing nc")

		bad, _ := newClient(t, Settings{
			BaseURL: srv.URL, Auth: AuthDigest, User: "admin",
			Password: "wrong",
		})
		_, err := bad.Get(context.Background(), "/x")
		require.ErrorIs(t, err, ErrUnauthorized)
	}
}

func TestBasicAndNoAuth(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		u, p, ok := r.BasicAuth()
		if r.URL.Path == "/open" || (ok && u == "admin" && p == "pw") {
			_, _ = w.Write([]byte("{}"))
			return
		}
		w.WriteHeader(nethttp.StatusUnauthorized)
	}))
	defer srv.Close()
	c, _ := newClient(t, Settings{
		BaseURL: srv.URL, Auth: AuthBasic, User: "admin",
		Password: "pw",
	})
	_, err := c.Get(context.Background(), "/status")
	require.NoError(t, err)
	anon, _ := newClient(t, Settings{BaseURL: srv.URL, Auth: AuthDigest})
	_, err = anon.Get(context.Background(), "/status")
	require.ErrorIs(t, err, ErrUnauthorized)
	_, err = anon.Get(context.Background(), "/open")
	require.NoError(t, err)
}

func TestRedirectsAndStatus(t *testing.T) {
	t.Parallel()
	other := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		_, _ = w.Write([]byte("evil"))
	}))
	defer other.Close()
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.URL.Path {
		case "/same":
			nethttp.Redirect(w, r, "/final", nethttp.StatusFound)
		case "/final":
			_, _ = w.Write([]byte("ok"))
		case "/away":
			nethttp.Redirect(w, r, other.URL, nethttp.StatusFound)
		case "/big":
			_, _ = w.Write(make([]byte, MaxBodySize+1))
		default:
			w.WriteHeader(nethttp.StatusNotFound)
		}
	}))
	defer srv.Close()
	c, _ := newClient(t, Settings{BaseURL: srv.URL})
	body, err := c.Get(context.Background(), "/same")
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
	_, err = c.Get(context.Background(), "/away")
	require.ErrorIs(t, err, ErrRedirect)
	_, err = c.Get(context.Background(), "/missing")
	require.ErrorIs(t, err, ErrStatus)
	_, err = c.Get(context.Background(), "/big")
	require.ErrorContains(t, err, "larger than")
	_, err = c.Get(context.Background(), "%zz")
	require.ErrorContains(t, err, "path")
	assert.ErrorIs(t, sameHost(nil, make([]*nethttp.Request, 3)), ErrRedirect)
}

func TestBackoffAndTLS(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		_, _ = w.Write([]byte("tls"))
	}))
	defer srv.Close()
	strict, clock := newClient(t, Settings{BaseURL: srv.URL})
	_, err := strict.Get(context.Background(), "/")
	require.ErrorIs(t, err, ErrUnavailable, "certificate not trusted")
	_, err = strict.Get(context.Background(), "/")
	require.ErrorContains(t, err, "certificate", "one failure is tolerated without a delay")
	_, err = strict.Get(context.Background(), "/")
	require.ErrorContains(t, err, "backoff")
	clock.Advance(BackoffMin)
	_, err = strict.Get(context.Background(), "/")
	require.ErrorContains(t, err, "certificate")

	insecure, _ := newClient(t, Settings{BaseURL: srv.URL, Insecure: true})
	body, err := insecure.Get(context.Background(), "/")
	require.NoError(t, err)
	assert.Equal(t, "tls", string(body))

	_, err = New(Settings{BaseURL: "::"}, clock, zerolog.Nop())
	require.Error(t, err)
	_, err = New(Settings{BaseURL: "http://x"}, nil, zerolog.Nop())
	require.Error(t, err)
}

func TestChallengeParsing(t *testing.T) {
	t.Parallel()
	assert.Nil(t, parseChallenge("Basic realm=x"))
	c := parseChallenge(`Digest realm="a,b", nonce="n", algorithm=MD5-sess`)
	require.NotNil(t, c)
	assert.Equal(t, "a,b", c.realm)
	assert.Empty(t, c.qop)
	auth := c.authorize("u", "p", "GET", "/x")
	assert.NotContains(t, auth, "qop=")
	assert.Contains(t, auth, "algorithm=MD5-sess")
	assert.Equal(t, "", firstQop("auth-int"))
}
