// Package configstore persists the UI configuration (spec 3): objects and settings with a
// revision (config_rev), history snapshots, audit entries with a diff, and encrypted
// secrets. Validation of the whole configuration is done by the caller (internal/app).
package configstore

import (
	"errors"
	"maps"
	"sort"
	"strings"
	"time"
)

// Kinds of UI objects.
const (
	KindConnection = "connection"
	KindDevice     = "device"
	KindMeter      = "meter"
)

// Masked is shown instead of a stored secret; sent back unchanged it keeps the secret.
const Masked = "***"

var (
	// ErrNotFound is returned for an unknown object.
	ErrNotFound = errors.New("configstore: not found")
	// ErrUnknownKind is returned for a kind other than connection, device or meter.
	ErrUnknownKind = errors.New("configstore: unknown kind")
	// ErrConflict is returned when another change was saved since the edited revision.
	ErrConflict = errors.New("configstore: configuration changed meanwhile")
	// ErrLocked is returned for objects and settings owned by config.yaml or env.
	ErrLocked = errors.New("configstore: defined in config.yaml")
)

// Section returns the config section of a kind ("device" → "devices").
func Section(kind string) (string, error) {
	switch kind {
	case KindConnection, KindDevice, KindMeter:
		return kind + "s", nil
	}
	return "", ErrUnknownKind
}

// Key names one object.
type Key struct{ Kind, ID string }

// Change is one object write of a batch (PUT /api/v1/config, the setup probe); a nil Body
// deletes the object.
type Change struct {
	Kind string         `json:"kind"`
	ID   string         `json:"id"`
	Body map[string]any `json:"body"`
}

// Object is one UI object with plain secrets.
type Object struct {
	Body       map[string]any // file form: {"connection": "lan", "auth": {"password": …}}
	UpdatedAt  time.Time
	UpdatedBy  string
	Unreadable []string // secret paths that could not be decrypted (lost secret.key)
	stored     string   // body as stored, reused while the object is unchanged
}

// Snapshot is the UI configuration at one revision. It is never changed in place: the
// With… methods of Store return a new one.
type Snapshot struct {
	Rev      int64
	Objects  map[Key]Object
	Settings map[string]any // flat keys: "site.name"
}

func (s *Snapshot) clone() *Snapshot {
	return &Snapshot{Rev: s.Rev, Objects: maps.Clone(s.Objects), Settings: maps.Clone(s.Settings)}
}

// Layer returns the snapshot as the DB layer of config.Load: objects nested under their
// section, settings as flat keys.
func (s *Snapshot) Layer() map[string]any {
	out := map[string]any{}
	for k, o := range s.Objects {
		sec, err := Section(k.Kind)
		if err != nil {
			continue
		}
		m, ok := out[sec].(map[string]any)
		if !ok {
			m = map[string]any{}
			out[sec] = m
		}
		m[k.ID] = copyMap(o.Body)
	}
	maps.Copy(out, s.Settings)
	return out
}

// Keys returns the object keys of a kind, sorted by ID.
func (s *Snapshot) Keys(kind string) []Key {
	var out []Key
	for k := range s.Objects {
		if k.Kind == kind {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// copyMap deep-copies nested maps; other values are shared (they are never modified).
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			v = copyMap(sub)
		}
		out[k] = v
	}
	return out
}

// getPath returns the value at a dotted path.
func getPath(m map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		sub, ok := m[p].(map[string]any)
		if !ok {
			return nil, false
		}
		m = sub
	}
	v, ok := m[parts[len(parts)-1]]
	return v, ok
}

// setPath sets the value at a dotted path, creating maps on the way.
func setPath(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		sub, ok := m[p].(map[string]any)
		if !ok {
			sub = map[string]any{}
			m[p] = sub
		}
		m = sub
	}
	m[parts[len(parts)-1]] = v
}

// flatten returns the leaves of m by dotted path.
func flatten(m map[string]any, prefix string, out map[string]any) map[string]any {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			flatten(sub, path, out)
			continue
		}
		out[path] = v
	}
	return out
}
