package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/netip"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/auth"
	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/internal/http/handler"
	"github.com/dombyte/pvmon/internal/http/middleware"
	"github.com/dombyte/pvmon/internal/http/router"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
)

// CreateAuth builds the auth service from auth.* (server and CLI jobs).
func CreateAuth(l *Loaded, st *storage.Storage, clock util.Clock, log zerolog.Logger,
) (*auth.Service, error) {
	c := l.Result.Config.Auth
	svc, err := auth.New(auth.Settings{
		SessionIdle: c.SessionIdle.D(), SessionMax: c.SessionMax.D(),
	}, auth.Deps{
		Store: st.Auth(), Clock: clock, Rand: rand.Reader,
		Log: log.With().Str("component", "auth").Logger(),
	})
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	return svc, nil
}

// httpHandler builds the auth service, logs the setup code while no user exists
// (spec 10) and returns the middleware chain around the runtime's routes.
func httpHandler(ctx context.Context, o ServeOptions, rt *runtime, log zerolog.Logger,
) (http.Handler, error) {
	svc, err := CreateAuth(o.Loaded, rt.d.st, o.Clock, log)
	if err != nil {
		return nil, err
	}
	setup, err := svc.SetupRequired(ctx)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	if setup {
		log.Warn().Str("setup_code", svc.SetupCode()).
			Msg("no admin account yet: open the UI and enter the setup code")
	}
	rt.auth = svc
	rt.mux.Store(rt.routes(rt.current()))
	inner := middleware.CSRF(log, middleware.Authenticate(svc, log, rt))
	cfg := o.Loaded.Result.Config
	return middleware.Proxy(trustedProxies(cfg.App.TrustedProxies), middleware.Headers(inner)),
		nil
}

func authHandlers(h *handler.Auth) router.AuthHandlers {
	return router.AuthHandlers{
		Login: h.Login, Logout: h.Logout, Me: h.Me, Setup: h.Setup,
		Users: h.Users, AddUser: h.AddUser, DeleteUser: h.DeleteUser,
		SetPassword: h.SetPassword, Tokens: h.Tokens, CreateToken: h.CreateToken,
		RevokeToken: h.RevokeToken, AuditLog: h.Audit,
	}
}

// trustedProxies parses app.trusted_proxies; Validate already rejected invalid entries.
func trustedProxies(list []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(list))
	for _, s := range list {
		if p, err := config.ParsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}
