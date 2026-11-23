package configstore

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dombyte/pvmon/internal/storage"
)

// Problem is one rejected field with its path.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidationError lists every problem of a change (HTTP 422).
type ValidationError struct{ Problems []Problem }

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.Path + ": " + p.Message
	}
	return "configstore: invalid: " + strings.Join(parts, "; ")
}

func prefixed(k Key, probs []Problem) []Problem {
	sec, _ := Section(k.Kind) //nolint:errcheck // kind checked by the caller
	for i := range probs {
		probs[i].Path = sec + "." + k.ID + "." + probs[i].Path
	}
	return probs
}

// change is one audit line: an object or setting with its changed fields.
type change struct {
	Kind    string            `json:"kind,omitempty"`
	ID      string            `json:"id,omitempty"`
	Op      string            `json:"op"`
	Changed map[string][2]any `json:"changed,omitempty"` // path → [old, new]
}

// diff turns the difference of two snapshots into a storage change and its audit detail
// (secrets masked).
func (sv *Service) diff(base, next *Snapshot, now time.Time, by string,
) (storage.ConfigChange, string, error) {
	ch := storage.ConfigChange{
		BaseRev: base.Rev, At: now.UnixMilli(), By: by, Set: map[string]string{},
	}
	lines, err := sv.diffObjects(&ch, base, next)
	if err != nil {
		return ch, "", err
	}
	settings, err := diffSettings(&ch, base.Settings, next.Settings)
	if err != nil {
		return ch, "", err
	}
	if settings.Changed != nil {
		lines = append(lines, settings)
	}
	detail, err := json.Marshal(map[string]any{"changes": lines})
	if err != nil {
		return ch, "", fmt.Errorf("configstore: audit: %w", err)
	}
	return ch, string(detail), nil
}

func sortedKeys(a, b map[Key]Object) []Key {
	set := map[Key]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	keys := slices.Collect(maps.Keys(set))
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Kind != keys[j].Kind {
			return keys[i].Kind < keys[j].Kind
		}
		return keys[i].ID < keys[j].ID
	})
	return keys
}

func (sv *Service) diffObjects(ch *storage.ConfigChange, base, next *Snapshot,
) ([]change, error) {
	var lines []change
	for _, k := range sortedKeys(base.Objects, next.Objects) {
		o, inNext := next.Objects[k]
		prev, inBase := base.Objects[k]
		if !inNext {
			ch.Delete = append(ch.Delete, storage.ConfigKey{Kind: k.Kind, ID: k.ID})
			lines = append(lines, change{Kind: k.Kind, ID: k.ID, Op: "delete"})
			continue
		}
		if inBase && reflect.DeepEqual(prev.Body, o.Body) {
			continue
		}
		put, err := sv.encode(k, o)
		if err != nil {
			return nil, err
		}
		ch.Put = append(ch.Put, put)
		lines = append(lines, change{
			Kind: k.Kind, ID: k.ID, Op: "put", Changed: sv.fields(k.Kind, prev.Body, o.Body),
		})
	}
	return lines, nil
}

// fields lists the changed leaves of an object; secrets show only that they changed.
func (sv *Service) fields(kind string, prev, next map[string]any) map[string][2]any {
	a := flatten(prev, "", map[string]any{})
	b := flatten(next, "", map[string]any{})
	out := map[string][2]any{}
	for path := range mergeKeys(a, b) {
		if reflect.DeepEqual(a[path], b[path]) {
			continue
		}
		pair := [2]any{a[path], b[path]}
		if slices.Contains(sv.s.Secrets[kind], path) {
			pair = [2]any{maskValue(a[path]), maskValue(b[path])}
		}
		out[path] = pair
	}
	return out
}

func maskValue(v any) any {
	if v == nil || v == "" {
		return v
	}
	return Masked
}

func mergeKeys(a, b map[string]any) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

func diffSettings(ch *storage.ConfigChange, base, next map[string]any) (change, error) {
	line := change{Op: "settings"}
	for k := range mergeKeys(base, next) {
		v, inNext := next[k]
		if reflect.DeepEqual(base[k], v) && inNext == (base[k] != nil) {
			continue
		}
		if line.Changed == nil {
			line.Changed = map[string][2]any{}
		}
		line.Changed[k] = [2]any{base[k], v}
		if !inNext {
			ch.Unset = append(ch.Unset, k)
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return line, fmt.Errorf("configstore: setting %s: %w", k, err)
		}
		ch.Set[k] = string(raw)
	}
	sort.Strings(ch.Unset)
	return line, nil
}

// encode serialises an object body with its secrets encrypted.
func (sv *Service) encode(k Key, o Object) (storage.ConfigObject, error) {
	body := copyMap(o.Body)
	for _, path := range sv.s.Secrets[k.Kind] {
		v, ok := getPath(body, path)
		s, isStr := v.(string)
		if !ok || !isStr || s == "" {
			continue
		}
		sealed, err := sv.d.Sealer.Seal(s)
		if err != nil {
			return storage.ConfigObject{}, fmt.Errorf("configstore: %w", err)
		}
		setPath(body, path, sealed)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return storage.ConfigObject{}, fmt.Errorf("configstore: %s %s: %w", k.Kind, k.ID, err)
	}
	return storage.ConfigObject{
		Kind: k.Kind, ID: k.ID, Body: string(raw),
		UpdatedAt: o.UpdatedAt.UnixMilli(), UpdatedBy: o.UpdatedBy,
	}, nil
}

// snapshotJSON is the history snapshot of next: stored bodies (secrets encrypted), the
// new bodies of put, and the settings.
func (sv *Service) snapshotJSON(next *Snapshot, put []storage.ConfigObject) (string, error) {
	bodies := map[string]map[string]json.RawMessage{}
	for k, o := range next.Objects {
		sec, _ := Section(k.Kind) //nolint:errcheck // kinds are checked on edit
		if bodies[sec] == nil {
			bodies[sec] = map[string]json.RawMessage{}
		}
		stored := o.stored
		if stored == "" { // built in memory, not loaded
			enc, err := sv.encode(k, o)
			if err != nil {
				return "", err
			}
			stored = enc.Body
		}
		bodies[sec][k.ID] = json.RawMessage(stored)
	}
	for _, p := range put {
		sec, _ := Section(p.Kind) //nolint:errcheck // kinds are checked on edit
		bodies[sec][p.ID] = json.RawMessage(p.Body)
	}
	raw, err := json.Marshal(map[string]any{"objects": bodies, "settings": next.Settings})
	if err != nil {
		return "", fmt.Errorf("configstore: snapshot: %w", err)
	}
	return string(raw), nil
}
