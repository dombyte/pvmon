package config

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrNoUnit is returned for durations, sizes and energies given without a unit.
var ErrNoUnit = errors.New("needs a unit")

const (
	hoursPerDay  = 24
	daysPerYear  = 365
	whPerKWh     = 1000
	whPerMWh     = 1000 * 1000
	bytesPerKB   = 1000
	maskedSecret = "***"
)

// Duration is a time.Duration that must be written with a unit; besides Go's units it
// accepts d (24h) and y (365d): "5s", "30d", "10y", "1h30m".
type Duration time.Duration

// D returns the value as time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

var durationPart = regexp.MustCompile(`(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h|d|y)`)

// ParseDuration parses a duration with unit, see Duration.
func ParseDuration(s string) (Duration, error) {
	s = strings.TrimSpace(s)
	parts := durationPart.FindAllStringSubmatchIndex(s, -1)
	if s == "" || len(parts) == 0 {
		return 0, fmt.Errorf("duration %q: %w", s, ErrNoUnit)
	}
	var total time.Duration
	pos := 0
	for _, p := range parts {
		if p[0] != pos {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		pos = p[1]
		v, err := durationValue(s[p[2]:p[3]], s[p[4]:p[5]])
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		total += v
	}
	if pos != len(s) {
		return 0, fmt.Errorf("duration %q: %w", s, ErrNoUnit)
	}
	return Duration(total), nil
}

func durationValue(num, unit string) (time.Duration, error) {
	switch unit {
	case "d", "y":
		f, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, fmt.Errorf("parse %q: %w", num, err)
		}
		days := f
		if unit == "y" {
			days *= daysPerYear
		}
		return time.Duration(days * hoursPerDay * float64(time.Hour)), nil
	default:
		d, err := time.ParseDuration(num + unit)
		if err != nil {
			return 0, fmt.Errorf("parse: %w", err)
		}
		return d, nil
	}
}

// String renders the duration in Go notation.
func (d Duration) String() string { return time.Duration(d).String() }

// Energy is an energy in Wh written with a unit: "10kWh", "500 Wh", "1.2MWh".
type Energy float64

// Wh returns the energy in watt-hours.
func (e Energy) Wh() float64 { return float64(e) }

var energyRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(Wh|kWh|MWh)$`)

// ParseEnergy parses an energy with unit.
func ParseEnergy(s string) (Energy, error) {
	m := energyRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("energy %q: %w (Wh, kWh, MWh)", s, ErrNoUnit)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, fmt.Errorf("energy %q: %w", s, err)
	}
	mult := map[string]float64{"Wh": 1, "kWh": whPerKWh, "MWh": whPerMWh}[m[2]]
	return Energy(v * mult), nil
}

// Size is a byte size written with a unit: "50MB", "512 KB", "1GB" (decimal units).
type Size int64

// MB returns the size in whole megabytes, at least 1 for a non-zero size.
func (s Size) MB() int {
	mb := int64(s) / (bytesPerKB * bytesPerKB)
	if mb == 0 && s > 0 {
		return 1
	}
	return int(mb)
}

var sizeRe = regexp.MustCompile(`^(\d+)\s*(B|KB|MB|GB)$`)

// ParseSize parses a byte size with unit.
func ParseSize(s string) (Size, error) {
	m := sizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("size %q: %w (B, KB, MB, GB)", s, ErrNoUnit)
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("size %q: %w", s, err)
	}
	mult := map[string]int64{
		"B": 1, "KB": bytesPerKB, "MB": bytesPerKB * bytesPerKB,
		"GB": bytesPerKB * bytesPerKB * bytesPerKB,
	}[m[2]]
	return Size(v * mult), nil
}

// Secret is a credential. Its value never appears in String, JSON or logs.
type Secret struct{ value string }

// NewSecret wraps a plain value.
func NewSecret(v string) Secret { return Secret{value: v} }

// Value returns the plain secret for the component that needs it.
func (s Secret) Value() string { return s.value }

// IsSet reports whether the secret has a value.
func (s Secret) IsSet() bool { return s.value != "" }

// String masks the secret.
func (s Secret) String() string {
	if s.value == "" {
		return ""
	}
	return maskedSecret
}

// MarshalJSON masks the secret.
func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(s.String())), nil
}

// MarshalText masks the secret (YAML export, zerolog).
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

var secretRef = regexp.MustCompile(`^\$\{(env|file):([^}]+)\}$`)

// IsSecretRef reports whether s is a ${env:NAME} or ${file:/path} reference.
func IsSecretRef(s string) bool { return secretRef.MatchString(s) }
