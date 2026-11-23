package config

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// Problem is one validation finding with the key path it refers to.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (p Problem) String() string { return p.Path + ": " + p.Message }

// Problems is a list of findings.
type Problems []Problem

// Err returns nil for no problems, else a *ValidationError sorted by path.
func (ps Problems) Err() error {
	if len(ps) == 0 {
		return nil
	}
	sorted := append(Problems(nil), ps...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Path != sorted[j].Path {
			return sorted[i].Path < sorted[j].Path
		}
		return sorted[i].Message < sorted[j].Message
	})
	// the structural checks and the site rules may report the same finding
	return &ValidationError{Problems: slices.CompactFunc(sorted, func(a, b Problem) bool {
		return a == b
	})}
}

// ValidationError carries every problem of a configuration.
type ValidationError struct {
	Problems Problems
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.String()
	}
	return "config: invalid: " + strings.Join(parts, "; ")
}

// Rule is a domain rule injected by internal/app (spec: config imports no domain package).
type Rule func(cfg *Config) Problems

// IDPattern is the pattern of connection, device and meter IDs.
var IDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

const (
	maxPort        = 65535
	minInterval    = time.Second
	minReqTimeout  = time.Second
	minObjectLimit = 0
)

// Validate runs the structural checks and every rule and reports all problems at once.
func Validate(cfg *Config, rules ...Rule) error {
	var ps Problems
	ps = append(ps, validateSettings(cfg)...)
	ps = append(ps, validateConnections(cfg)...)
	ps = append(ps, validateDevices(cfg)...)
	ps = append(ps, validateMeters(cfg)...)
	for _, r := range rules {
		ps = append(ps, r(cfg)...)
	}
	return ps.Err()
}

type check struct {
	bad  bool
	path string
	msg  string
}

func collect(checks ...check) Problems {
	var out Problems
	for _, c := range checks {
		if c.bad {
			out = append(out, Problem{Path: c.path, Message: c.msg})
		}
	}
	return out
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func validateSettings(c *Config) Problems {
	ps := collect(
		check{c.App.Port < 1 || c.App.Port > maxPort, "app.port", "must be 1..65535"},
		check{
			!oneOf(c.App.LogLevel, "debug", "info", "warn", "error"), "app.log_level",
			"must be debug, info, warn or error",
		},
		check{c.App.RequestTimeout.D() < minReqTimeout, "app.request_timeout", "must be >= 1s"},
		check{c.Auth.SessionIdle.D() <= 0, "auth.session_idle", "must be > 0"},
		check{
			c.Auth.SessionMax.D() < c.Auth.SessionIdle.D(), "auth.session_max",
			"must be >= auth.session_idle",
		},
		check{c.Storage.Path == "", "storage.path", "must not be empty"},
		check{c.Storage.Retention.D() <= 0, "storage.retention", "must be > 0"},
		check{c.Storage.BackupInterval.D() <= 0, "storage.backup_interval", "must be > 0"},
		check{c.Storage.MaxBackups < 1, "storage.max_backups", "must be >= 1"},
		check{c.Status.OfflineAfter.D() <= 0, "status.offline_after", "must be > 0"},
		check{c.Site.Interval.D() < minInterval, "site.interval", "must be >= 1s"},
		check{c.Log.MaxSize <= 0, "log.max_size", "must be > 0"},
		check{c.Log.MaxBackups < minObjectLimit, "log.max_backups", "must be >= 0"},
		check{
			!oneOf(c.Log.Compression, "gzip", "zstd", "none"), "log.compression",
			"must be gzip, zstd or none",
		},
	)
	for i, at := range c.Log.RotateAt {
		if !IsClockTime(at) {
			ps = append(ps, Problem{
				Path:    fmt.Sprintf("log.rotate_at.%d", i),
				Message: fmt.Sprintf("%q is not HH:MM", at),
			})
		}
	}
	return append(ps, validateProxies(c.App.TrustedProxies)...)
}

func validateProxies(list []string) Problems {
	var ps Problems
	for i, p := range list {
		if _, err := ParsePrefix(p); err != nil {
			ps = append(ps, Problem{
				Path:    fmt.Sprintf("app.trusted_proxies.%d", i),
				Message: fmt.Sprintf("%q is no IP address or CIDR prefix", p),
			})
		}
	}
	return ps
}

// ParsePrefix reads a trusted proxy as an IP address (a single-address prefix) or CIDR.
func ParsePrefix(s string) (netip.Prefix, error) {
	if a, err := netip.ParseAddr(s); err == nil {
		return netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()), nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("config: %w", err)
	}
	return p.Masked(), nil
}

var clockTime = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// IsClockTime reports whether s is a strict HH:MM time of day.
func IsClockTime(s string) bool { return clockTime.MatchString(s) }

func validateIDs[T any](kind string, m map[string]T) Problems {
	var ps Problems
	for id := range m {
		if !IDPattern.MatchString(id) {
			ps = append(ps, Problem{
				Path:    kind + "." + id,
				Message: "ID must match [a-z][a-z0-9-]{0,31}",
			})
		}
	}
	return ps
}

func validateConnections(c *Config) Problems {
	ps := validateIDs("connections", c.Connections)
	for id, conn := range c.Connections {
		p := "connections." + id
		ps = append(ps, collect(
			check{conn.Address == "", p + ".address", "must not be empty"},
			check{conn.Timeout.D() <= 0, p + ".timeout", "must be > 0"},
			check{!oneOf(conn.Parity, "N", "E", "O"), p + ".parity", "must be N, E or O"},
		)...)
	}
	return ps
}

func validateDevices(c *Config) Problems {
	ps := validateIDs("devices", c.Devices)
	for id, d := range c.Devices {
		ps = append(ps, validateDevice(c, "devices."+id, d)...)
	}
	return ps
}

func validateDevice(c *Config, p string, d Device) Problems {
	_, connOK := c.Connections[d.Connection]
	return collect(
		check{
			(d.Connection == "") == (d.Address == ""), p,
			"set exactly one of connection and address",
		},
		check{
			d.Connection != "" && !connOK, p + ".connection",
			fmt.Sprintf("unknown connection %q", d.Connection),
		},
		check{d.Profile == "", p + ".profile", "must not be empty"},
		check{
			d.Interval.D() != 0 && d.Interval.D() < minInterval, p + ".interval",
			"must be 0 (site interval) or >= 1s",
		},
		check{d.Timeout.D() < 0, p + ".timeout", "must be >= 0"},
		check{!oneOf(d.Read, "", "range", "direct"), p + ".read", "must be range or direct"},
		check{!oneOf(d.Netting, "", "meter", "app"), p + ".netting", "must be meter or app"},
		check{
			d.Rollover != "" && !IsClockTime(d.Rollover), p + ".rollover",
			fmt.Sprintf("%q is not HH:MM", d.Rollover),
		},
	)
}

func validateMeters(c *Config) Problems {
	ps := validateIDs("meters", c.Meters)
	for id, m := range c.Meters {
		p := "meters." + id
		_, devOK := c.Devices[m.Device]
		ps = append(ps, collect(
			check{m.Device == "", p + ".device", "must not be empty"},
			check{
				m.Device != "" && !devOK, p + ".device",
				fmt.Sprintf("unknown device %q", m.Device),
			},
			check{m.Source == "", p + ".source", "must not be empty"},
			check{m.KWp < 0, p + ".kwp", "must be >= 0"},
			check{m.Capacity < 0, p + ".capacity", "must be >= 0"},
		)...)
	}
	return ps
}
