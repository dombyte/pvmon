// Package http is the HTTP(S) JSON transport: GET requests only, basic and digest
// authentication, no redirects to other hosts, per-request timeouts and backoff after
// failures (spec 4.4, 10). It never sends anything but GET.
package http

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/util"
)

// Authentication schemes.
const (
	AuthNone   = ""
	AuthBasic  = "basic"
	AuthDigest = "digest"
)

// Backoff bounds and response limit.
const (
	BackoffMin  = time.Second
	BackoffMax  = 30 * time.Second
	MaxBodySize = 1 << 20 // 1 MiB
	idleTimeout = 90 * time.Second
)

// Errors returned by Get.
var (
	ErrUnavailable  = errors.New("http: device unavailable")
	ErrUnauthorized = errors.New("http: unauthorized")
	ErrStatus       = errors.New("http: unexpected status")
	ErrRedirect     = errors.New("http: redirect to another host refused")
)

// Settings describes one device endpoint base.
type Settings struct {
	BaseURL  string // scheme://host[:port]
	Timeout  time.Duration
	Auth     string
	User     string
	Password string // sent to the device only, never logged
	Insecure bool   // skip TLS verification (insecure_tls)
}

// Reader fetches a path relative to the base URL.
type Reader interface {
	Get(ctx context.Context, path string) ([]byte, error)
}

// Client is the HTTP transport of one device. Get is called by one goroutine at a time
// (the collector of the connection).
type Client struct {
	s       Settings
	base    *url.URL
	clock   util.Clock
	http    *nethttp.Client
	log     zerolog.Logger
	backoff util.Backoff
	digest  *challenge // last digest challenge, reused with increasing nc
}

// New builds a client; it does not contact the device.
func New(s Settings, clock util.Clock, log zerolog.Logger) (*Client, error) {
	if err := util.RequireAll("http", util.D("clock", clock)); err != nil {
		return nil, err
	}
	base, err := url.Parse(s.BaseURL)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("http: invalid base URL %q", s.BaseURL)
	}
	tr := &nethttp.Transport{
		TLSHandshakeTimeout: s.Timeout, MaxIdleConnsPerHost: 1, IdleConnTimeout: idleTimeout,
	}
	if s.Insecure {
		// insecure_tls is an explicit per-device opt-in (spec 10)
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in
	}
	c := &Client{
		s: s, base: base, clock: clock,
		log:     log.With().Str("component", "http").Str("address", base.Host).Logger(),
		backoff: util.Backoff{Min: BackoffMin, Max: BackoffMax, Grace: 1},
	}
	c.http = &nethttp.Client{Transport: tr, Timeout: s.Timeout, CheckRedirect: sameHost}
	return c, nil
}

// sameHost allows redirects only within the original host.
func sameHost(req *nethttp.Request, via []*nethttp.Request) error {
	const maxRedirects = 3
	if len(via) >= maxRedirects || req.URL.Host != via[0].URL.Host {
		return ErrRedirect
	}
	return nil
}

// Get fetches path (with query). During backoff it fails fast with ErrUnavailable.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	if now := c.clock.Now(); !c.backoff.Ready(now) {
		return nil, fmt.Errorf("%w: backoff until %s", ErrUnavailable,
			c.backoff.Next().Format(time.TimeOnly))
	}
	body, err := c.get(ctx, path)
	if errors.Is(err, ErrUnavailable) {
		d := c.backoff.Fail(c.clock.Now())
		c.log.Debug().Err(err).Dur("backoff", d).Msg("request failed, backing off")
		return nil, err
	}
	if err == nil {
		c.backoff.Reset()
	}
	return body, err
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	u, err := c.base.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("http: path %q: %w", path, err)
	}
	resp, err := c.fetch(ctx, u)
	if err == nil && resp.status == nethttp.StatusUnauthorized && c.s.Auth == AuthDigest &&
		c.s.User != "" {
		c.digest = parseChallenge(resp.header.Get("WWW-Authenticate"))
		resp, err = c.fetch(ctx, u)
	}
	if err != nil {
		return nil, err
	}
	return resp.check()
}

// response is a fully read reply.
type response struct {
	status int
	header nethttp.Header
	body   []byte
}

func (r response) check() ([]byte, error) {
	switch {
	case r.status == nethttp.StatusUnauthorized:
		return nil, ErrUnauthorized
	case r.status < nethttp.StatusOK || r.status >= nethttp.StatusMultipleChoices:
		return nil, fmt.Errorf("%w: %d", ErrStatus, r.status)
	case len(r.body) > MaxBodySize:
		return nil, fmt.Errorf("%w: body larger than %d bytes", ErrStatus, MaxBodySize)
	}
	return r.body, nil
}

// fetch sends one GET with the current authorization and reads the whole reply.
func (c *Client) fetch(ctx context.Context, u *url.URL) (response, error) {
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, u.String(), nil)
	if err != nil {
		return response{}, fmt.Errorf("http: request: %w", err)
	}
	if auth := c.authorization(u); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, ErrRedirect) {
			return response{}, err
		}
		return response{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer c.closeBody(resp)
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodySize+1))
	if err != nil {
		return response{}, fmt.Errorf("%w: read body: %w", ErrUnavailable, err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: body}, nil
}

func (c *Client) closeBody(resp *nethttp.Response) {
	if err := resp.Body.Close(); err != nil {
		c.log.Debug().Err(err).Msg("close response body")
	}
}

func (c *Client) authorization(u *url.URL) string {
	switch {
	case c.s.User == "":
		return ""
	case c.s.Auth == AuthBasic:
		return basicAuth(c.s.User, c.s.Password)
	case c.s.Auth == AuthDigest && c.digest != nil:
		return c.digest.authorize(c.s.User, c.s.Password, nethttp.MethodGet, u.RequestURI())
	}
	return ""
}
