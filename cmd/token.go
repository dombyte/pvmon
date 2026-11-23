package main

import (
	"fmt"
	"time"

	"github.com/dombyte/pvmon/internal/auth"
	"github.com/dombyte/pvmon/internal/config"
)

// runToken implements "pvmon token create|list|revoke" (spec 13).
func runToken(e env) error {
	fs, cfg := newFlags("token", e.stderr)
	name := fs.String("name", "", "token name")
	expires := fs.String("expires", "", "lifetime with unit, e.g. 90d (default: never)")
	var d time.Duration
	return dispatch(e, fs, cfg, map[string]subcommand{
		"list": {run: func(e env, svc *auth.Service, _ []string) error {
			return listTokens(e, svc)
		}},
		"create": {
			write: true,
			pre: func(env) (err error) {
				if *name == "" {
					return fmt.Errorf("token create needs --name: %w", errUsage)
				}
				d, err = parseExpiry(*expires)
				return err
			},
			run: func(e env, svc *auth.Service, _ []string) error {
				return createToken(e, svc, *name, d)
			},
		},
		"revoke": {args: 1, write: true, run: revokeToken},
	}, "token create --name n [--expires 90d] | token list | token revoke <id>")
}

func revokeToken(e env, svc *auth.Service, pos []string) error {
	if err := svc.RevokeToken(e.ctx, auth.CLI(), pos[0]); err != nil {
		return fmt.Errorf("token %s: %w", pos[0], err)
	}
	writeLine(e.stdout, "token "+pos[0]+" revoked")
	return nil
}

func parseExpiry(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := config.ParseDuration(s)
	if err != nil || d.D() <= 0 {
		return 0, fmt.Errorf("--expires %q: want a positive duration with unit: %w", s, errUsage)
	}
	return d.D(), nil
}

func listTokens(e env, svc *auth.Service) error {
	ts, err := svc.Tokens(e.ctx)
	if err != nil {
		return err
	}
	for _, t := range ts {
		writeLine(e.stdout, fmt.Sprintf("%-8s %-24s %-5s created %s, expires %s, last used %s",
			t.ID, t.Name, t.Scope, t.CreatedAt.Local().Format(time.RFC3339), orNever(t.ExpiresAt),
			orNever(t.LastUsed)))
	}
	return nil
}

func orNever(t *time.Time) string {
	if t == nil {
		return "never"
	}
	return t.Local().Format(time.RFC3339)
}

func createToken(e env, svc *auth.Service, name string, d time.Duration) error {
	t, err := svc.CreateToken(e.ctx, auth.CLI(), name, d)
	if err != nil {
		return err
	}
	writeLine(e.stderr, "token created; it is shown only once:")
	writeLine(e.stdout, t.Token)
	return nil
}
