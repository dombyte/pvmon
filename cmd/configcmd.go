package main

import (
	"fmt"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/internal/util"
)

const configUsage = "config validate|export [-config path]"

// runConfig implements "pvmon config validate|export [-config path]" (spec 13): the file
// and env layers over the UI configuration stored in the database.
func runConfig(e env) error {
	if len(e.args) == 0 || (e.args[0] != "validate" && e.args[0] != "export") {
		return fmt.Errorf("%s: %w", configUsage, errUsage)
	}
	l, err := loadEffective(e)
	if err != nil {
		return err
	}
	if e.args[0] == "validate" {
		writeLine(e.stdout, fmt.Sprintf("configuration valid (config_rev %d)", l.Site.Rev))
		return nil
	}
	return exportConfig(e, l)
}

func exportConfig(e env, l *app.Loaded) error {
	out, err := config.Export(l.Result)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if _, err := e.stdout.Write(out); err != nil {
		return fmt.Errorf("config: write: %w", err)
	}
	return nil
}

// loadEffective loads the file and env layers, prints their warnings and adds the UI
// configuration from the database.
func loadEffective(e env) (*app.Loaded, error) {
	fs, cfg := newFlags("config "+e.args[0], e.stderr)
	if err := fs.Parse(e.args[1:]); err != nil || fs.NArg() > 0 {
		return nil, fmt.Errorf("%s: %w", configUsage, errUsage)
	}
	l, err := loadConfig(*cfg)
	if err != nil {
		return nil, err
	}
	for _, w := range l.Result.Warnings {
		writeLine(e.stderr, "warning: "+w)
	}
	l, err = app.LoadStored(e.ctx, l, util.NewRealClock())
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return l, nil
}
