package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/internal/logging"
	"github.com/dombyte/pvmon/profiles"
)

// defaultConfigPath is used when -config is not given; a missing default file is allowed.
const defaultConfigPath = "config.yaml"

// newFlags returns a flag set with the common -config flag.
func newFlags(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("config", defaultConfigPath, "configuration file")
	return fs, path
}

// loadConfig loads the configuration; the default path may be absent (defaults + env only).
func loadConfig(path string) (*app.Loaded, error) { return loadConfigEnv(path) }

// loadConfigEnv loads with extra environment entries that win over os.Environ.
func loadConfigEnv(path string, extra ...string) (*app.Loaded, error) {
	if path == defaultConfigPath {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			path = ""
		}
	}
	src := config.Source{File: path, Environ: append(os.Environ(), extra...)}
	l, err := app.LoadConfig(src, profiles.FS())
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return l, nil
}

// cliLogger logs human-oriented JSON lines of CLI jobs to stderr.
func cliLogger(stderr io.Writer, l *app.Loaded) (*logging.Logger, error) {
	log, err := logging.New(stderr, logging.Settings{Level: l.Result.Config.App.LogLevel})
	if err != nil {
		return nil, fmt.Errorf("logger: %w", err)
	}
	for _, w := range l.Result.Warnings {
		log.Warn().Msg(w)
	}
	return log, nil
}
