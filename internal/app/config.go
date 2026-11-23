// Package app is the composition root: it loads the configuration, maps it onto the
// Settings and Specs of every package and builds all components (Create* factories).
// Together with cmd it is the only importer of internal/config.
package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
)

// Loaded is the validated configuration with the site built from it.
type Loaded struct {
	Result *config.Result
	Site   *site.Site

	src     config.Source // the layers it was loaded from, for WithDB
	builtin fs.FS
}

// LoadConfig loads the layered configuration and builds the site; the site rules run as
// config.Rules so every problem is reported at once with its path.
func LoadConfig(src config.Source, builtin fs.FS) (*Loaded, error) {
	dir := "."
	if src.File != "" {
		dir = filepath.Dir(src.File)
	}
	return loadConfig(src, builtin, func(string) string { return dir })
}

// loadConfig loads with profileDir giving the base directory of a device's relative
// profile path.
func loadConfig(src config.Source, builtin fs.FS, profileDir func(device string) string,
) (*Loaded, error) {
	loader, err := profile.NewLoader(builtin, os.ReadFile)
	if err != nil {
		return nil, fmt.Errorf("app: profiles: %w", err)
	}
	var built *site.Site
	rule := func(cfg *config.Config) config.Problems {
		s, probs := buildSite(cfg, profileDir, loader)
		built = s
		return probs
	}
	res, err := config.Load(src, rule)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	return &Loaded{Result: res, Site: built, src: src, builtin: builtin}, nil
}

// WithDB loads the same file and env layers again over the UI configuration layer of
// revision rev (spec 3: defaults → DB → file → env). Relative profile paths of devices
// from the UI layer are resolved against the data directory (spec 4.5).
func (l *Loaded) WithDB(layer map[string]any, rev int64) (*Loaded, error) {
	src := l.src
	src.DB = layer
	fileDir, dataDir := l.Result.Dir, filepath.Dir(l.Result.Config.Storage.Path)
	next, err := loadConfig(src, l.builtin, func(device string) string {
		if l.Result.Locked("devices." + device) {
			return fileDir
		}
		return dataDir
	})
	if err != nil {
		return nil, err
	}
	next.Site.Rev = rev
	return next, nil
}

// defaultTimeout applies to devices with an inline address and no timeout (the default of
// a connection's timeout, spec 4.3).
const defaultTimeout = 2 * time.Second

// buildSite resolves profiles and builds the site; problems carry config paths.
func buildSite(cfg *config.Config, profileDir func(device string) string,
	loader *profile.Loader,
) (*site.Site, config.Problems) {
	var probs config.Problems
	spec := site.Spec{
		Name:        cfg.Site.Name,
		Connections: map[string]site.ConnectionSpec{},
		Devices:     map[string]site.DeviceSpec{},
		Meters:      map[string]site.MeterSpec{},
	}
	for id, c := range cfg.Connections {
		spec.Connections[id] = connectionSpec(c)
	}
	for id, d := range cfg.Devices {
		p, err := loader.Load(d.Profile, profileDir(id))
		if err != nil {
			probs = append(probs, config.Problem{
				Path:    "devices." + id + ".profile",
				Message: err.Error(),
			})
		}
		spec.Devices[id] = deviceSpec(d, p, cfg.Site.Interval.D())
	}
	for id, m := range cfg.Meters {
		spec.Meters[id] = meterSpec(m)
	}
	s, err := site.Build(spec)
	var se *site.Error
	if errors.As(err, &se) {
		for _, p := range se.Problems {
			probs = append(probs, config.Problem{Path: p.Path, Message: p.Message})
		}
	}
	return s, probs
}

func connectionSpec(c config.Connection) site.ConnectionSpec {
	return site.ConnectionSpec{
		Address: c.Address, Timeout: c.Timeout.D(),
		Serial: site.Serial{
			Speed: c.Speed, DataBits: c.DataBits, Parity: c.Parity,
			StopBits: c.StopBits,
		},
	}
}

// deviceSpec maps a device; it polls at the site interval unless it sets its own.
func deviceSpec(d config.Device, p *profile.Loaded, siteInterval time.Duration) site.DeviceSpec {
	interval := d.Interval.D()
	if interval == 0 {
		interval = siteInterval
	}
	timeout := d.Timeout.D()
	if d.Connection == "" && timeout == 0 {
		timeout = defaultTimeout
	}
	return site.DeviceSpec{
		Connection: d.Connection, Address: d.Address, Unit: d.Unit, Profile: p,
		Interval: interval, Timeout: timeout, Read: d.Read, Rollover: d.Rollover,
		Netting: d.Netting, History: d.History, User: d.Auth.User,
		Password: d.Auth.Password.Value(), Insecure: d.InsecureTLS,
	}
}

func meterSpec(m config.Meter) site.MeterSpec {
	return site.MeterSpec{
		Device: m.Device, Source: m.Source, Role: m.Role, Category: m.Category,
		Parent: m.Parent, Label: m.Label, KWp: m.KWp, CapacityWh: m.Capacity.Wh(),
		Critical: m.Critical,
	}
}
