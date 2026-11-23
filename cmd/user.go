package main

import (
	"bufio"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/auth"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
)

// errNoPassword means stdin held no password line.
var errNoPassword = errors.New("no password on stdin")

// authJob runs fn with the auth service while the server is stopped (exclusive lock, spec
// 13); jobs that write make and verify a backup first.
func authJob(e env, cfgPath string, write bool, fn func(svc *auth.Service) error,
) (err error) {
	l, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	lock, err := lockDB(l.Result.Config.Storage.Path)
	if err != nil {
		return err
	}
	defer releaseLock(lock, e.stderr)
	clock := util.NewRealClock()
	db, err := app.OpenJobDB(e.ctx, l, clock.Now())
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if write {
		if err := backupAuth(e, l, db, clock.Now()); err != nil {
			return err
		}
	}
	svc, err := newAuthService(e, l, db, clock)
	if err != nil {
		return err
	}
	return fn(svc)
}

func backupAuth(e env, l *app.Loaded, db *sql.DB, now time.Time) error {
	path, err := app.BackupJobDB(e.ctx, l, db, now)
	if err != nil {
		return err
	}
	writeLine(e.stderr, "backup: "+path)
	return nil
}

func newAuthService(e env, l *app.Loaded, db *sql.DB, clock util.Clock) (*auth.Service, error) {
	st, err := storage.New(db, app.LocalLocation())
	if err != nil {
		return nil, err
	}
	log, err := cliLogger(e.stderr, l)
	if err != nil {
		return nil, err
	}
	return app.CreateAuth(l, st, clock, log.Logger)
}

// subcommand is one action of "pvmon user" or "pvmon token".
type subcommand struct {
	args  int  // number of positional arguments
	write bool // needs a backup first
	// pre validates the input before the lock and backup; nil = nothing to check.
	pre func(e env) error
	run func(e env, svc *auth.Service, pos []string) error
}

// dispatch parses the flags of fs and runs the named subcommand as an auth job.
func dispatch(e env, fs *flag.FlagSet, cfg *string, subs map[string]subcommand, use string,
) error {
	if len(e.args) == 0 {
		return fmt.Errorf("%s: %w", use, errUsage)
	}
	pos, err := parseArgs(fs, e.args[1:])
	sub, ok := subs[e.args[0]]
	if err != nil || !ok || len(pos) != sub.args {
		return fmt.Errorf("%s: %w", use, errUsage)
	}
	if sub.pre != nil {
		if err := sub.pre(e); err != nil {
			return err
		}
	}
	return authJob(e, *cfg, sub.write, func(svc *auth.Service) error {
		return sub.run(e, svc, pos)
	})
}

// parseArgs parses flags before and after the positional arguments.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, errUsage
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// runUser implements "pvmon user add|reset-password|list" (spec 13).
func runUser(e env) error {
	fs, cfg := newFlags("user", e.stderr)
	lang := fs.String("lang", "", "UI language of a new user (de, en)")
	var pw string
	read := func(e env) (err error) {
		pw, err = readPassword(e)
		return err
	}
	return dispatch(e, fs, cfg, map[string]subcommand{
		"list": {run: func(e env, svc *auth.Service, _ []string) error {
			return listUsers(e, svc)
		}},
		"add": {
			args: 1, write: true, pre: read,
			run: func(e env, svc *auth.Service, pos []string) error {
				return addUser(e, svc, pos[0], pw, *lang)
			},
		},
		"reset-password": {
			args: 1, write: true, pre: read,
			run: func(e env, svc *auth.Service, pos []string) error {
				return resetPassword(e, svc, pos[0], pw)
			},
		},
	}, "user add|reset-password <name> [--lang de|en] | user list")
}

func listUsers(e env, svc *auth.Service) error {
	us, err := svc.Users(e.ctx)
	if err != nil {
		return err
	}
	for _, u := range us {
		last := "never"
		if u.LastLogin != nil {
			last = u.LastLogin.Local().Format(time.RFC3339)
		}
		writeLine(e.stdout, fmt.Sprintf("%-4d %-24s %-3s created %s, last login %s", u.ID,
			u.Name, u.Lang, u.CreatedAt.Local().Format(time.RFC3339), last))
	}
	return nil
}

func addUser(e env, svc *auth.Service, name, pw, lang string) error {
	u, err := svc.AddUser(e.ctx, auth.CLI(), name, pw, lang)
	if err != nil {
		return err
	}
	writeLine(e.stdout, fmt.Sprintf("user %s created (id %d)", u.Name, u.ID))
	return nil
}

func resetPassword(e env, svc *auth.Service, name, pw string) error {
	u, err := svc.UserByName(e.ctx, name)
	if err != nil {
		return fmt.Errorf("user %s: %w", name, err)
	}
	if err := svc.SetPassword(e.ctx, auth.CLI(), u.ID, pw); err != nil {
		return err
	}
	writeLine(e.stdout, fmt.Sprintf("password of %s changed, all sessions ended", u.Name))
	return nil
}

// readPassword reads the first line of stdin and checks the policy, e.g.
// printf '%s\n' "$PW" | pvmon user add a.
// Without a terminal library the input is not hidden, so piping it in is the safe way.
func readPassword(e env) (string, error) {
	if e.stdin == nil {
		return "", errNoPassword
	}
	_, _ = fmt.Fprintf(e.stderr, "new password (min. %d characters): ", auth.MinPasswordLen)
	line, err := bufio.NewReader(e.stdin).ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		if errors.Is(err, io.EOF) {
			err = nil
		}
		return "", errors.Join(errNoPassword, err)
	}
	return line, auth.CheckPassword(line)
}
