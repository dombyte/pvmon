package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	yamlv3 "go.yaml.in/yaml/v3"
)

// EnvPrefix is the prefix of environment overrides. Nesting uses a double underscore:
// PVMON_APP__LOG_LEVEL → app.log_level, PVMON_DEVICES__WR1__INTERVAL → devices.wr1.interval.
const EnvPrefix = "PVMON_"

const delim = "."

// Source describes the layers to load. Zero fields are skipped; Getenv and ReadFile default
// to the os functions and resolve ${env:…} / ${file:…} secret references.
type Source struct {
	File     string         // config.yaml path, "" = none
	Data     []byte         // file layer given as YAML bytes instead of File (simulate)
	DB       map[string]any // UI layer from configstore (flat or nested keys)
	Environ  []string       // KEY=VALUE pairs, normally os.Environ()
	Getenv   func(string) (string, bool)
	ReadFile func(string) ([]byte, error)
}

// Result is a loaded and validated configuration.
type Result struct {
	Config   *Config
	Dir      string         // directory of the config file; base for relative profile paths
	FileKeys []string       // keys defined in the file layer (read-only in the UI)
	EnvKeys  []string       // keys set by environment variables (read-only in the UI)
	Layers   map[string]any // merged layers without defaults, secrets as given (export)
	Warnings []string       // unknown or removed keys
}

// Locked reports whether path (a setting such as "site.name" or an object such as
// "devices.wr1") is defined by the file or env layer and therefore read-only in the UI.
func (r *Result) Locked(path string) bool {
	for _, keys := range [][]string{r.FileKeys, r.EnvKeys} {
		for _, k := range keys {
			if k == path || strings.HasPrefix(k, path+delim) {
				return true
			}
		}
	}
	return false
}

// Load merges defaults → DB → file → env, decodes and validates. Problems are returned as
// one *ValidationError listing every problem with its path.
func Load(src Source, rules ...Rule) (*Result, error) {
	src = withOSDefaults(src)
	layers, keys, err := loadLayers(src)
	if err != nil {
		return nil, err
	}
	res := &Result{
		FileKeys: keys.file, EnvKeys: keys.env, Layers: layers.Raw(),
		Warnings: unknownKeys(layers), Dir: configDir(src.File),
	}
	full := withDefaults(layers)
	var problems Problems
	problems = append(problems, checkFileSecrets(src.File, keys.file, layers)...)
	problems = append(problems, checkTyped(full)...)
	problems = append(problems, checkSecrets(full, src)...)
	if len(problems) > 0 {
		return nil, problems.Err()
	}
	cfg := &Config{}
	if err := decode(full, cfg, src); err != nil {
		return nil, err
	}
	if err := Validate(cfg, rules...); err != nil {
		return nil, err
	}
	res.Config = cfg
	return res, nil
}

func withOSDefaults(src Source) Source {
	if src.Getenv == nil {
		src.Getenv = os.LookupEnv
	}
	if src.ReadFile == nil {
		src.ReadFile = os.ReadFile
	}
	return src
}

func configDir(path string) string {
	if path == "" {
		return "."
	}
	return filepath.Dir(path)
}

// layerKeys are the keys set by the file and the env layer.
type layerKeys struct{ file, env []string }

// loadLayers merges DB, file and env into one koanf. An object defined in the file belongs
// entirely to the file (spec 3): the DB layer's object with the same ID is dropped.
func loadLayers(src Source) (*koanf.Koanf, layerKeys, error) {
	var keys layerKeys
	kf := koanf.New(delim)
	if src.File != "" || src.Data != nil {
		var err error
		if kf, err = loadFileLayer(src); err != nil {
			return nil, keys, err
		}
		keys.file = kf.Keys()
	}
	k, err := loadDBLayer(src, kf)
	if err != nil {
		return nil, keys, err
	}
	ke, err := loadEnvLayer(src)
	if err != nil {
		return nil, keys, err
	}
	keys.env = ke.Keys()
	_ = k.Merge(kf) //nolint:errcheck // merge of maps never fails
	_ = k.Merge(ke) //nolint:errcheck // merge of maps never fails
	return k, keys, nil
}

// loadDBLayer loads the DB layer without the objects the file layer kf defines.
func loadDBLayer(src Source, kf *koanf.Koanf) (*koanf.Koanf, error) {
	k := koanf.New(delim)
	if err := k.Load(confmap.Provider(src.DB, delim), nil); err != nil {
		return nil, fmt.Errorf("config: load db layer: %w", err)
	}
	for _, sec := range objectSections() {
		for _, id := range kf.MapKeys(sec) {
			k.Delete(sec + delim + id)
		}
	}
	return k, nil
}

func loadEnvLayer(src Source) (*koanf.Koanf, error) {
	ke := koanf.New(delim)
	envp := env.Provider(delim, env.Opt{
		Prefix:        EnvPrefix,
		TransformFunc: envKey,
		EnvironFunc:   func() []string { return src.Environ },
	})
	if err := ke.Load(envp, nil); err != nil {
		return nil, fmt.Errorf("config: load env layer: %w", err)
	}
	return ke, nil
}

// objectSections are the sections that hold objects by ID.
func objectSections() []string { return []string{"connections", "devices", "meters"} }

// loadFileLayer reads the file layer from the file or from YAML bytes.
func loadFileLayer(src Source) (*koanf.Koanf, error) {
	kf := koanf.New(delim)
	if src.Data != nil {
		var m map[string]any
		if err := yamlv3.Unmarshal(src.Data, &m); err != nil {
			return nil, fmt.Errorf("config: parse data: %w", err)
		}
		if err := kf.Load(confmap.Provider(m, delim), nil); err != nil {
			return nil, fmt.Errorf("config: load data: %w", err)
		}
		return kf, nil
	}
	if err := kf.Load(file.Provider(src.File), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("config: load %s: %w", src.File, err)
	}
	return kf, nil
}

func envKey(k, v string) (string, any) {
	k = strings.TrimPrefix(k, EnvPrefix)
	return strings.ReplaceAll(strings.ToLower(k), "__", delim), v
}

// unknownKeys lists keys that match no Config field.
func unknownKeys(k *koanf.Koanf) []string {
	known := KnownKeys()
	var out []string
	for _, key := range k.Keys() {
		if !matchesAny(known, key) {
			out = append(out, fmt.Sprintf("unknown or removed key %q is ignored", key))
		}
	}
	return out
}

func matchesAny(patterns []string, key string) bool {
	for _, p := range patterns {
		if matchKey(p, key) {
			return true
		}
	}
	return false
}

// withDefaults returns defaults (global and per object) overlaid by the layers.
func withDefaults(layers *koanf.Koanf) *koanf.Koanf {
	defs := Defaults()
	for kind, objDefs := range ObjectDefaults() {
		for _, id := range objectIDs(layers, kind) {
			for key, v := range objDefs {
				defs[kind+delim+id+delim+key] = v
			}
		}
	}
	full := koanf.New(delim)
	_ = full.Load(confmap.Provider(defs, delim), nil) //nolint:errcheck // confmap never fails
	_ = full.Merge(layers)                            //nolint:errcheck // merge of maps never fails
	return full
}

// objectIDs returns the sorted object IDs of a kind present in k.
func objectIDs(k *koanf.Koanf, kind string) []string {
	m := k.MapKeys(kind)
	sort.Strings(m)
	return m
}

// checkFileSecrets rejects plain secrets in the file layer: they must be references.
func checkFileSecrets(path string, fileKeys []string, k *koanf.Koanf) Problems {
	var out Problems
	for _, key := range fileKeys {
		if !isSecretKey(key) {
			continue
		}
		if v := k.String(key); v != "" && !IsSecretRef(v) {
			out = append(out, Problem{Path: key, Message: fmt.Sprintf(
				"secrets in %s must be ${env:NAME} or ${file:/path} references", path)})
		}
	}
	return out
}

func isSecretKey(key string) bool {
	t, ok := typeOf(key)
	return ok && t == reflect.TypeFor[Secret]()
}

func typeOf(key string) (reflect.Type, bool) {
	for pattern, t := range keyTypes() {
		if matchKey(pattern, key) {
			return t, true
		}
	}
	return nil, false
}

// checkSecrets resolves every secret reference once so failures carry their path.
func checkSecrets(k *koanf.Koanf, src Source) Problems {
	var out Problems
	for _, key := range k.Keys() {
		if !isSecretKey(key) {
			continue
		}
		if _, err := resolveSecret(k.String(key), src); err != nil {
			out = append(out, Problem{Path: key, Message: err.Error()})
		}
	}
	return out
}

// checkTyped parses every Duration, Energy and Size value so problems carry their path.
func checkTyped(k *koanf.Koanf) Problems {
	var out Problems
	for _, key := range k.Keys() {
		t, ok := typeOf(key)
		if !ok {
			continue
		}
		if err := parseTyped(t, k.Get(key)); err != nil {
			out = append(out, Problem{Path: key, Message: err.Error()})
		}
	}
	return out
}

func parseTyped(t reflect.Type, v any) error {
	parser := typedParsers()[t]
	if parser == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("%v: %w", v, ErrNoUnit)
	}
	_, err := parser(s)
	return err
}

func typedParsers() map[reflect.Type]func(string) (any, error) {
	return map[reflect.Type]func(string) (any, error){
		reflect.TypeFor[Duration](): func(s string) (any, error) { return ParseDuration(s) },
		reflect.TypeFor[Energy]():   func(s string) (any, error) { return ParseEnergy(s) },
		reflect.TypeFor[Size]():     func(s string) (any, error) { return ParseSize(s) },
	}
}

// decode unmarshals k into cfg; typed values were checked before, secrets are resolved.
func decode(k *koanf.Koanf, cfg *Config, src Source) error {
	dc := &mapstructure.DecoderConfig{
		DecodeHook:       decodeHook(src),
		WeaklyTypedInput: true,
		Result:           cfg,
		TagName:          "koanf",
	}
	if err := k.UnmarshalWithConf("", cfg, koanf.UnmarshalConf{DecoderConfig: dc}); err != nil {
		return fmt.Errorf("config: decode: %w", err)
	}
	return nil
}

func decodeHook(src Source) mapstructure.DecodeHookFuncType {
	parsers := typedParsers()
	return func(from, to reflect.Type, data any) (any, error) {
		s, ok := data.(string)
		if !ok || from.Kind() != reflect.String {
			return data, nil
		}
		if p := parsers[to]; p != nil {
			return p(s)
		}
		if to == reflect.TypeFor[Secret]() {
			return resolveSecret(s, src)
		}
		return data, nil
	}
}

// resolveSecret turns ${env:NAME} / ${file:/path} into the secret; other values are taken
// as they are (DB layer after decryption, env layer).
func resolveSecret(s string, src Source) (Secret, error) {
	m := secretRef.FindStringSubmatch(s)
	if m == nil {
		return NewSecret(s), nil
	}
	if m[1] == "env" {
		v, ok := src.Getenv(m[2])
		if !ok {
			return Secret{}, fmt.Errorf("config: secret: environment variable %s not set", m[2])
		}
		return NewSecret(v), nil
	}
	b, err := src.ReadFile(m[2])
	if err != nil {
		return Secret{}, fmt.Errorf("config: secret file: %w", err)
	}
	return NewSecret(strings.TrimRight(string(b), "\r\n")), nil
}
