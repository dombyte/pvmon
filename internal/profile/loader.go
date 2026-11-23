package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// ErrUnknownProfile is returned for a reference that is neither a path nor a built-in ID.
var ErrUnknownProfile = errors.New("unknown profile")

// Loaded is a profile resolved for a device. Path is empty for built-in profiles.
type Loaded struct {
	*Profile
	Path      string
	Overrides bool // a local file replacing the built-in profile with the same ID
}

// Loader resolves profile references: built-in IDs from the embedded set, or local files
// by path (spec 4.5). It holds no cache of local files; they are read on every Load.
type Loader struct {
	builtin  map[string]*Profile
	readFile func(string) ([]byte, error)
}

// NewLoader parses every built-in profile; any invalid or duplicate one is an error.
func NewLoader(builtin fs.FS, readFile func(string) ([]byte, error)) (*Loader, error) {
	if builtin == nil || readFile == nil {
		return nil, errors.New("profile: loader needs the built-in FS and a file reader")
	}
	l := &Loader{builtin: map[string]*Profile{}, readFile: readFile}
	paths, err := fs.Glob(builtin, "*/*.yaml")
	if err != nil {
		return nil, fmt.Errorf("profile: list built-in profiles: %w", err)
	}
	var errs []error
	for _, path := range paths {
		errs = append(errs, l.addBuiltin(builtin, path))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Loader) addBuiltin(fsys fs.FS, path string) error {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return fmt.Errorf("profile: read built-in %s: %w", path, err)
	}
	p, err := Parse(data)
	if err != nil {
		return fmt.Errorf("profile: built-in %s: %w", path, err)
	}
	if want := strings.TrimSuffix(path, ".yaml"); p.ID != want {
		return fmt.Errorf("profile: built-in %s declares ID %q", path, p.ID)
	}
	l.builtin[p.ID] = p
	return nil
}

// IsPath reports whether a profile reference names a local file (ends in .yaml/.yml).
func IsPath(ref string) bool {
	return strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml")
}

// IDs returns the sorted built-in profile IDs.
func (l *Loader) IDs() []string {
	ids := make([]string, 0, len(l.builtin))
	for id := range l.builtin {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Load resolves ref; relative paths are resolved against baseDir. An unreadable or invalid
// local file is an error, never a fallback to the built-in profile.
func (l *Loader) Load(ref, baseDir string) (*Loaded, error) {
	if !IsPath(ref) {
		p, ok := l.builtin[ref]
		if !ok {
			return nil, fmt.Errorf("profile: %q: %w", ref, ErrUnknownProfile)
		}
		return &Loaded{Profile: p}, nil
	}
	path := ref
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	p, err := l.LoadFile(path)
	if err != nil {
		return nil, err
	}
	_, overrides := l.builtin[p.ID]
	return &Loaded{Profile: p, Path: path, Overrides: overrides}, nil
}

// LoadFile reads and validates one local profile file.
func (l *Loader) LoadFile(path string) (*Profile, error) {
	data, err := l.readFile(path)
	if err != nil {
		return nil, fmt.Errorf("profile: read %s: %w", path, err)
	}
	p, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("profile: %s: %w", path, err)
	}
	return p, nil
}
