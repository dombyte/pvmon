package app

import (
	"context"
	"errors"
	"os"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/internal/configstore"
	"github.com/dombyte/pvmon/internal/http/handler"
	"github.com/dombyte/pvmon/internal/http/router"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/service"
)

func (r *runtime) collectorDeps() collector.Deps {
	return collector.Deps{
		Store: r.d.st.Collector(), Cache: r.d.cache, Bus: r.d.bus, Reporter: r.sup,
		Clock: r.o.Clock, Log: r.log,
	}
}

func (r *runtime) configHandlers(l *Loaded) router.ConfigHandlers {
	loader, err := profile.NewLoader(l.builtin, os.ReadFile)
	if err != nil { // the same profiles were loaded at start
		r.log.Error().Err(err).Msg("profiles")
	}
	h := handler.NewConfig(r, service.NewProfiles(loader), r.log)
	return router.ConfigHandlers{
		Overview: h.Overview, Settings: h.Settings, PutSettings: h.PutSettings,
		Export: h.Export, Object: h.Object, PutObject: h.PutObject,
		DeleteObject: h.DeleteObject, Profiles: h.Profiles, PutObjects: h.PutObjects,
		Probe: h.Probe,
	}
}

// Overview implements handler.ConfigService: objects of the UI layer and the objects
// defined in config.yaml or env (locked), plus every UI setting.
func (r *runtime) Overview(context.Context) (configstore.Overview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := r.current()
	ov := configstore.Overview{
		Rev: r.snap.Rev, Objects: []configstore.ObjectView{},
		Settings: map[string]configstore.SettingView{},
	}
	for _, kind := range []string{
		configstore.KindConnection, configstore.KindDevice, configstore.KindMeter,
	} {
		sec, _ := configstore.Section(kind) //nolint:errcheck // known kinds
		for _, id := range l.Result.ObjectIDs(sec) {
			if v, err := r.view(l, kind, id); err == nil {
				ov.Objects = append(ov.Objects, v)
			}
		}
	}
	for _, k := range config.UISettingKeys() {
		_, stored := r.snap.Settings[k]
		ov.Settings[k] = configstore.SettingView{
			Value: l.Result.Value(k), Locked: l.Result.Locked(k), Stored: stored,
		}
	}
	return ov, nil
}

// view returns one object: from the file/env layer when locked, else from the UI layer.
func (r *runtime) view(l *Loaded, kind, id string) (configstore.ObjectView, error) {
	sec, err := configstore.Section(kind)
	if err != nil {
		return configstore.ObjectView{}, err
	}
	if l.Result.Locked(sec + "." + id) {
		body, _ := l.Result.Object(sec, id)
		return configstore.ObjectView{Kind: kind, ID: id, Locked: true, Body: body}, nil
	}
	k := configstore.Key{Kind: kind, ID: id}
	o, ok := r.snap.Objects[k]
	if !ok {
		return configstore.ObjectView{}, configstore.ErrNotFound
	}
	return r.cs.View(k, o), nil
}

// Object implements handler.ConfigService.
func (r *runtime) Object(_ context.Context, kind, id string) (configstore.ObjectView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.view(r.current(), kind, id)
}

// PutObject implements handler.ConfigService.
func (r *runtime) PutObject(ctx context.Context, by, kind, id string, body map[string]any,
) (configstore.ObjectView, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.editable(kind, id); err != nil {
		return configstore.ObjectView{}, 0, err
	}
	k := configstore.Key{Kind: kind, ID: id}
	next, err := r.cs.WithObject(r.snap, k, body, by)
	if err != nil {
		return configstore.ObjectView{}, 0, err
	}
	if err := r.commit(ctx, next, by); err != nil {
		return configstore.ObjectView{}, 0, err
	}
	v, err := r.view(r.current(), kind, id)
	return v, r.snap.Rev, err
}

// DeleteObject implements handler.ConfigService.
func (r *runtime) DeleteObject(ctx context.Context, by, kind, id string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.editable(kind, id); err != nil {
		return 0, err
	}
	next, err := r.cs.WithoutObject(r.snap, configstore.Key{Kind: kind, ID: id})
	if err != nil {
		return 0, err
	}
	if err := r.commit(ctx, next, by); err != nil {
		return 0, err
	}
	return r.snap.Rev, nil
}

// editable checks kind and ID and refuses objects owned by the file or env layer.
func (r *runtime) editable(kind, id string) error {
	sec, err := configstore.Section(kind)
	if err != nil {
		return err
	}
	if !config.IDPattern.MatchString(id) {
		return &configstore.ValidationError{Problems: []configstore.Problem{{
			Path: sec + "." + id, Message: "ID must match " + config.IDPattern.String(),
		}}}
	}
	if r.current().Result.Locked(sec + "." + id) {
		return configstore.ErrLocked
	}
	return nil
}

// PutSettings implements handler.ConfigService.
func (r *runtime) PutSettings(ctx context.Context, by string, set map[string]any,
) (configstore.Overview, error) {
	if err := r.putSettings(ctx, by, set); err != nil {
		return configstore.Overview{}, err
	}
	return r.Overview(ctx)
}

func (r *runtime) putSettings(ctx context.Context, by string, set map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range set {
		if r.current().Result.Locked(k) {
			return configstore.ErrLocked
		}
	}
	next, err := r.cs.WithSettings(r.snap, set)
	if err != nil {
		return err
	}
	return r.commit(ctx, next, by)
}

// Export implements handler.ConfigService.
func (r *runtime) Export(context.Context) ([]byte, error) {
	out, err := config.Export(r.current().Result)
	if err != nil {
		return nil, err //nolint:wrapcheck // config errors carry their prefix
	}
	return out, nil
}

// commit validates next as a whole, stores it and applies it; the caller holds mu.
func (r *runtime) commit(ctx context.Context, next *configstore.Snapshot, by string) error {
	prev := r.current()
	l, err := r.validated(next)
	if err != nil {
		return err
	}
	rev, err := r.cs.Commit(ctx, r.snap, next, by)
	if err != nil || rev == r.snap.Rev {
		return err //nolint:wrapcheck // configstore errors carry their prefix
	}
	snap, err := r.cs.Load(ctx)
	if err != nil {
		return err //nolint:wrapcheck // configstore errors carry their prefix
	}
	r.snap = snap
	l.Site.Rev = rev
	r.apply(prev, l)
	return nil
}

// validated is the effective configuration over the UI layer of next, or every problem
// of it; the caller holds mu.
func (r *runtime) validated(next *configstore.Snapshot) (*Loaded, error) {
	layer := next.Layer()
	probs := config.CheckLayer(layer)
	l, err := r.current().WithDB(layer, 0)
	var ve *config.ValidationError
	if errors.As(err, &ve) {
		probs = append(probs, ve.Problems...)
	} else if err != nil {
		return nil, err
	}
	if len(probs) > 0 {
		return nil, problemsError(probs)
	}
	return l, nil
}

// PutObjects implements handler.ConfigService: several object changes as one revision, so
// the setup wizard can save a site that is only valid as a whole.
func (r *runtime) PutObjects(ctx context.Context, by string, cs []configstore.Change,
) (configstore.Overview, error) {
	if err := r.putObjects(ctx, by, cs); err != nil {
		return configstore.Overview{}, err
	}
	return r.Overview(ctx)
}

func (r *runtime) putObjects(ctx context.Context, by string, cs []configstore.Change) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next, err := r.withChanges(r.snap, cs, by)
	if err != nil {
		return err
	}
	return r.commit(ctx, next, by)
}

// withChanges applies the changes in order to a copy of base; the caller holds mu.
func (r *runtime) withChanges(base *configstore.Snapshot, cs []configstore.Change, by string,
) (*configstore.Snapshot, error) {
	next := base
	for _, c := range cs {
		if err := r.editable(c.Kind, c.ID); err != nil {
			return nil, err
		}
		k := configstore.Key{Kind: c.Kind, ID: c.ID}
		var err error
		if c.Body == nil {
			next, err = r.cs.WithoutObject(next, k)
		} else {
			next, err = r.cs.WithObject(next, k, c.Body, by)
		}
		if err != nil {
			return nil, err //nolint:wrapcheck // configstore errors carry their prefix
		}
	}
	return next, nil
}

func problemsError(ps config.Problems) error {
	var ve *config.ValidationError
	errors.As(ps.Err(), &ve) // sorted and without duplicates
	out := make([]configstore.Problem, len(ve.Problems))
	for i, p := range ve.Problems {
		out[i] = configstore.Problem{Path: p.Path, Message: p.Message}
	}
	return &configstore.ValidationError{Problems: out}
}
