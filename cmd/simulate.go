package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/internal/simulate"
	"github.com/dombyte/pvmon/profiles"
)

// runSimulate runs the server with simulated devices (spec 13). With --setup a built-in
// reference setup is used; otherwise the configured site, writing to simulate.db.
func runSimulate(e env) error {
	fs, cfg := newFlags("simulate", e.stderr)
	setup := fs.String("setup", "", "built-in setup (R1 to R7)")
	if err := fs.Parse(e.args); err != nil || fs.NArg() > 0 {
		return fmt.Errorf("simulate [--setup R1..R7] [-config path]: %w", errUsage)
	}
	l, err := loadSimulation(*setup, *cfg)
	if err != nil {
		return err
	}
	loc := app.LocalLocation()
	return app.Serve(e.ctx, app.ServeOptions{
		Loaded: l, Build: e.build, Stdout: e.stdout, Location: loc,
		Sources: simulate.Sources(loc), Probe: simulate.Probe(loc),
	})
}

func loadSimulation(setup, path string) (*app.Loaded, error) {
	if setup == "" {
		l, err := loadConfig(path)
		if err != nil {
			return nil, err
		}
		// The simulation keeps its own database next to the real one; set through env so
		// a reload with the UI layer keeps it.
		sim := filepath.Join(filepath.Dir(l.Result.Config.Storage.Path), "simulate.db")
		return loadConfigEnv(path, config.EnvPrefix+"STORAGE__PATH="+sim)
	}
	data, err := simulate.Setup(setup)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", err, errUsage)
	}
	l, err := app.LoadConfig(config.Source{Data: data, Environ: os.Environ()}, profiles.FS())
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return l, nil
}
