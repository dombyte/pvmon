package auth

import "context"

// SessionCookie is the name of the session cookie (spec 10).
const SessionCookie = "pvmon_session"

// Client describes where a request came from, resolved by the proxy middleware.
type Client struct {
	IP    string // remote address, or the forwarded client of a trusted proxy
	HTTPS bool   // direct TLS or X-Forwarded-Proto https from a trusted proxy
}

type clientKey struct{}

// WithClient returns ctx carrying c.
func WithClient(ctx context.Context, c Client) context.Context {
	return context.WithValue(ctx, clientKey{}, c)
}

// ClientFrom returns the client of ctx; the zero Client when none was set.
func ClientFrom(ctx context.Context) Client {
	c, _ := ctx.Value(clientKey{}).(Client) //nolint:errcheck // zero value is the answer
	return c
}
