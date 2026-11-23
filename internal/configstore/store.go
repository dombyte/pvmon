package configstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
)

// ActionChange is the audit action of a saved UI change.
const ActionChange = "config_change"

// Store is the interface of storage the config service needs.
type Store interface {
	ConfigState(ctx context.Context) (storage.ConfigState, error)
	ApplyConfig(ctx context.Context, c storage.ConfigChange) (int64, error)
}

// Sealer encrypts and decrypts secrets (*Cipher).
type Sealer interface {
	Seal(plain string) (string, error)
	Open(sealed string) (string, error)
}

// Settings describe the schema parts the store needs to know.
type Settings struct {
	// Secrets lists the secret paths inside an object body per kind ("auth.password").
	Secrets map[string][]string
	// SettingKeys are the settings the UI may change.
	SettingKeys []string
}

// Deps are the dependencies of Store.
type Deps struct {
	Store  Store
	Sealer Sealer
	Clock  util.Clock
}

// Service loads, edits and commits the UI configuration.
type Service struct {
	s Settings
	d Deps
}

// New builds the service.
func New(s Settings, d Deps) (*Service, error) {
	if err := util.RequireAll("configstore", util.D("store", d.Store),
		util.D("sealer", d.Sealer), util.D("clock", d.Clock)); err != nil {
		return nil, err
	}
	return &Service{s: s, d: d}, nil
}

// Load reads the stored UI configuration and decrypts its secrets; a secret that cannot
// be decrypted is empty and listed in Object.Unreadable.
func (sv *Service) Load(ctx context.Context) (*Snapshot, error) {
	st, err := sv.d.Store.ConfigState(ctx)
	if err != nil {
		return nil, fmt.Errorf("configstore: %w", err)
	}
	snap := &Snapshot{Rev: st.Rev, Objects: map[Key]Object{}, Settings: map[string]any{}}
	for _, o := range st.Objects {
		obj, err := sv.decode(o)
		if err != nil {
			return nil, err
		}
		snap.Objects[Key{Kind: o.Kind, ID: o.ID}] = obj
	}
	for k, raw := range st.Settings {
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("configstore: setting %s: %w", k, err)
		}
		snap.Settings[k] = v
	}
	return snap, nil
}

func (sv *Service) decode(o storage.ConfigObject) (Object, error) {
	obj := Object{
		UpdatedAt: time.UnixMilli(o.UpdatedAt), UpdatedBy: o.UpdatedBy, stored: o.Body,
	}
	if err := json.Unmarshal([]byte(o.Body), &obj.Body); err != nil {
		return obj, fmt.Errorf("configstore: %s %s: %w", o.Kind, o.ID, err)
	}
	for _, path := range sv.s.Secrets[o.Kind] {
		v, ok := getPath(obj.Body, path)
		s, isStr := v.(string)
		if !ok || !isStr || !strings.HasPrefix(s, encPrefix) {
			continue
		}
		plain, err := sv.d.Sealer.Open(s)
		if err != nil {
			obj.Unreadable = append(obj.Unreadable, path)
		}
		setPath(obj.Body, path, plain)
	}
	return obj, nil
}

// Masked returns the body of o with every set secret replaced by Masked.
func (sv *Service) Masked(kind string, o Object) map[string]any {
	body := copyMap(o.Body)
	for _, path := range sv.s.Secrets[kind] {
		if v, ok := getPath(body, path); ok && v != "" && v != nil {
			setPath(body, path, Masked)
		}
	}
	return body
}

// WithObject returns base with the object k set to body. A secret sent as Masked keeps
// the stored value; secret references (${env:…}, ${file:…}) are refused, because they
// would let the UI read server files and variables.
func (sv *Service) WithObject(base *Snapshot, k Key, body map[string]any, by string,
) (*Snapshot, error) {
	if _, err := Section(k.Kind); err != nil {
		return nil, err
	}
	body = copyMap(body)
	old, exists := base.Objects[k]
	var probs []Problem
	for _, path := range sv.s.Secrets[k.Kind] {
		probs = append(probs, keepSecret(body, old, exists, path)...)
	}
	if len(probs) > 0 {
		return nil, &ValidationError{Problems: prefixed(k, probs)}
	}
	next := base.clone()
	next.Objects[k] = Object{Body: body, UpdatedAt: sv.d.Clock.Now(), UpdatedBy: by}
	return next, nil
}

func keepSecret(body map[string]any, old Object, exists bool, path string) []Problem {
	v, ok := getPath(body, path)
	if !ok {
		return nil
	}
	s, isStr := v.(string)
	if !isStr {
		return []Problem{{Path: path, Message: "must be a string"}}
	}
	if strings.HasPrefix(s, "${") {
		return []Problem{{Path: path, Message: "references are only allowed in config.yaml"}}
	}
	if s != Masked {
		return nil
	}
	if !exists || slices.Contains(old.Unreadable, path) {
		return []Problem{{Path: path, Message: "enter the secret again"}}
	}
	prev, _ := getPath(old.Body, path)
	setPath(body, path, prev)
	return nil
}

// WithoutObject returns base without the object k.
func (sv *Service) WithoutObject(base *Snapshot, k Key) (*Snapshot, error) {
	if _, err := Section(k.Kind); err != nil {
		return nil, err
	}
	if _, ok := base.Objects[k]; !ok {
		return nil, ErrNotFound
	}
	next := base.clone()
	delete(next.Objects, k)
	return next, nil
}

// WithSettings returns base with the given settings; a nil value removes the setting
// (back to the default).
func (sv *Service) WithSettings(base *Snapshot, set map[string]any) (*Snapshot, error) {
	var probs []Problem
	next := base.clone()
	for k, v := range set {
		switch {
		case !slices.Contains(sv.s.SettingKeys, k):
			probs = append(probs, Problem{Path: k, Message: "cannot be changed in the UI"})
		case v == nil:
			delete(next.Settings, k)
		default:
			next.Settings[k] = v
		}
	}
	if len(probs) > 0 {
		return nil, &ValidationError{Problems: probs}
	}
	return next, nil
}

// Commit stores next as the change from base and returns the new revision; without a
// difference nothing is written. ErrConflict when another change was saved since base.
func (sv *Service) Commit(ctx context.Context, base, next *Snapshot, by string,
) (int64, error) {
	now := sv.d.Clock.Now()
	ch, detail, err := sv.diff(base, next, now, by)
	if err != nil {
		return 0, err
	}
	if len(ch.Put)+len(ch.Delete)+len(ch.Set)+len(ch.Unset) == 0 {
		return base.Rev, nil
	}
	if ch.Snapshot, err = sv.snapshotJSON(next, ch.Put); err != nil {
		return 0, err
	}
	ch.Audit = storage.AuditEntry{
		At: now.UnixMilli(), Actor: by, Action: ActionChange, Detail: detail,
	}
	rev, err := sv.d.Store.ApplyConfig(ctx, ch)
	if errors.Is(err, storage.ErrConflict) {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, fmt.Errorf("configstore: %w", err)
	}
	return rev, nil
}
