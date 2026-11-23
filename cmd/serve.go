package main

import (
	"fmt"

	"github.com/dombyte/pvmon/internal/app"
)

// runServe runs the server until a signal (exit 0) or a fatal error (exit 1).
func runServe(e env) error {
	fs, cfg := newFlags("serve", e.stderr)
	if err := fs.Parse(e.args); err != nil || fs.NArg() > 0 {
		return fmt.Errorf("serve [-config path]: %w", errUsage)
	}
	l, err := loadConfig(*cfg)
	if err != nil {
		return err
	}
	return app.Serve(e.ctx, app.ServeOptions{Loaded: l, Build: e.build, Stdout: e.stdout})
}
