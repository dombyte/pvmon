package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	yamlv3 "go.yaml.in/yaml/v3"
)

// UISettingKeys are the settings the UI may change. They apply without restart; the rest
// (port, paths, logging, sessions, proxies) only in config.yaml or env.
func UISettingKeys() []string {
	return []string{
		"site.name", "site.interval", "auth.public_read", "status.offline_after",
		"storage.retention", "storage.backup_interval", "storage.max_backups",
	}
}

// SecretPaths returns the secret key paths inside the objects of each section
// ("devices" → ["auth.password"]).
func SecretPaths() map[string][]string {
	out := map[string][]string{}
	for key := range keyTypes() {
		if !isSecretKey(key) {
			continue
		}
		parts := strings.SplitN(key, delim, 3) //nolint:mnd // section, id, path
		if len(parts) == 3 && parts[1] == Wildcard {
			out[parts[0]] = append(out[parts[0]], parts[2])
		}
	}
	return out
}

// CheckLayer reports the keys of a DB layer that match no configuration key, so the UI
// cannot store keys that would only be warned about.
func CheckLayer(layer map[string]any) Problems {
	k := koanf.New(delim)
	if err := k.Load(confmap.Provider(layer, delim), nil); err != nil {
		return Problems{{Path: "", Message: err.Error()}}
	}
	known := KnownKeys()
	var out Problems
	for _, key := range k.Keys() {
		if !matchesAny(known, key) {
			out = append(out, Problem{Path: key, Message: "unknown key"})
		}
	}
	return out
}

// Export renders the configured layers (without defaults) as YAML in the file form.
// Secrets are masked; references (${env:…}, ${file:…}) are kept.
func Export(r *Result) ([]byte, error) {
	k := koanf.New(delim)
	if err := k.Load(confmap.Provider(r.Layers, delim), nil); err != nil {
		return nil, fmt.Errorf("config: export: %w", err)
	}
	for _, key := range k.Keys() {
		if v := k.String(key); isSecretKey(key) && v != "" && !IsSecretRef(v) {
			_ = k.Set(key, maskedSecret) //nolint:errcheck // set on a loaded map never fails
		}
	}
	out, err := yamlv3.Marshal(k.Raw())
	if err != nil {
		return nil, fmt.Errorf("config: export: %w", err)
	}
	return out, nil
}

// Object returns the configured body of an object from the merged layers, secrets masked
// (references kept), or false.
func (r *Result) Object(section, id string) (map[string]any, bool) {
	k := koanf.New(delim)
	if err := k.Load(confmap.Provider(r.Layers, delim), nil); err != nil {
		return nil, false
	}
	path := section + delim + id
	if !k.Exists(path) {
		return nil, false
	}
	obj := k.Cut(path)
	for _, key := range obj.Keys() {
		v := obj.String(key)
		if isSecretKey(path+delim+key) && v != "" && !IsSecretRef(v) {
			_ = obj.Set(key, maskedSecret) //nolint:errcheck // set on a loaded map never fails
		}
	}
	return obj.Raw(), true
}

// Value returns the effective value of a global key: configured or default.
func (r *Result) Value(key string) any {
	k := koanf.New(delim)
	if err := k.Load(confmap.Provider(r.Layers, delim), nil); err == nil && k.Exists(key) {
		return typed(key, k.Get(key))
	}
	return Defaults()[key]
}

// typed turns an env string of a bool or int key into its type, so the UI gets the
// same JSON type whichever layer set the key; anything that does not parse stays as is.
func typed(key string, v any) any {
	s, ok := v.(string)
	t := keyTypes()[key]
	if !ok || t == nil {
		return v
	}
	switch t.Kind() { //nolint:exhaustive // only env strings of scalars
	case reflect.Bool:
		if b, err := strconv.ParseBool(s); err == nil {
			return b
		}
	case reflect.Int:
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return v
}

// ObjectIDs returns the IDs of a section's objects in the merged layers.
func (r *Result) ObjectIDs(section string) []string {
	k := koanf.New(delim)
	if err := k.Load(confmap.Provider(r.Layers, delim), nil); err != nil {
		return nil
	}
	return objectIDs(k, section)
}
