package config

import (
	"reflect"
	"sort"
	"strings"
)

// Wildcard stands for an object ID in key paths ("devices.*.interval").
const Wildcard = "*"

// KnownKeys returns every leaf key path of Config, with Wildcard for object IDs.
func KnownKeys() []string {
	types := keyTypes()
	out := make([]string, 0, len(types))
	for k := range types {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// keyTypes maps every leaf key path of Config to its Go type.
func keyTypes() map[string]reflect.Type {
	out := map[string]reflect.Type{}
	walkKeys(reflect.TypeFor[Config](), "", out)
	return out
}

func walkKeys(t reflect.Type, prefix string, out map[string]reflect.Type) {
	for i := range t.NumField() {
		f := t.Field(i)
		path := join(prefix, f.Tag.Get("koanf"))
		switch {
		case f.Type.Kind() == reflect.Map:
			walkKeys(f.Type.Elem(), join(path, Wildcard), out)
		case f.Type.Kind() == reflect.Struct && !isLeaf(f.Type):
			walkKeys(f.Type, path, out)
		default:
			out[path] = f.Type
		}
	}
}

// isLeaf reports whether a struct type is a scalar config value (Secret).
func isLeaf(t reflect.Type) bool { return t == reflect.TypeFor[Secret]() }

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// matchKey reports whether a concrete key matches a pattern with wildcards.
func matchKey(pattern, key string) bool {
	p := strings.Split(pattern, ".")
	k := strings.Split(key, ".")
	if len(p) != len(k) {
		return false
	}
	for i := range p {
		if p[i] != Wildcard && p[i] != k[i] {
			return false
		}
	}
	return true
}
