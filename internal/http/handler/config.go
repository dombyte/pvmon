package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/auth"
	"github.com/dombyte/pvmon/internal/configstore"
	"github.com/dombyte/pvmon/internal/service"
)

// ConfigService is what the configuration handlers need. Writes validate the whole
// configuration and apply it without restart (spec 3).
type ConfigService interface {
	Overview(ctx context.Context) (configstore.Overview, error)
	Object(ctx context.Context, kind, id string) (configstore.ObjectView, error)
	PutObject(ctx context.Context, by, kind, id string, body map[string]any,
	) (configstore.ObjectView, int64, error)
	DeleteObject(ctx context.Context, by, kind, id string) (int64, error)
	PutSettings(ctx context.Context, by string, set map[string]any,
	) (configstore.Overview, error)
	Export(ctx context.Context) ([]byte, error)
	PutObjects(ctx context.Context, by string, cs []configstore.Change,
	) (configstore.Overview, error)
	Probe(ctx context.Context, by string, cs []configstore.Change, device string,
	) (service.ProbeResult, error)
}

// ProfileLister lists the built-in profiles.
type ProfileLister interface {
	List() []service.ProfileInfo
}

// Config serves /api/v1/config/* and /api/v1/profiles (admin only).
type Config struct {
	svc      ConfigService
	profiles ProfileLister
	log      zerolog.Logger
}

// NewConfig builds the handlers.
func NewConfig(svc ConfigService, profiles ProfileLister, log zerolog.Logger) *Config {
	return &Config{svc: svc, profiles: profiles, log: log}
}

type objectBody struct {
	Rev    int64                  `json:"config_rev"`
	Object configstore.ObjectView `json:"object"`
}

type settingsBody struct {
	Rev      int64                              `json:"config_rev"`
	Settings map[string]configstore.SettingView `json:"settings"`
}

type profilesBody struct {
	Profiles []service.ProfileInfo `json:"profiles"`
}

// Overview serves GET /api/v1/config: every object and setting.
func (h *Config) Overview(w http.ResponseWriter, r *http.Request) {
	ov, err := h.svc.Overview(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ov, h.log)
}

// Settings serves GET /api/v1/config/settings.
func (h *Config) Settings(w http.ResponseWriter, r *http.Request) {
	ov, err := h.svc.Overview(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsBody{Rev: ov.Rev, Settings: ov.Settings}, h.log)
}

// PutSettings serves PUT /api/v1/config/settings: {key: value}, null resets a key.
func (h *Config) PutSettings(w http.ResponseWriter, r *http.Request) {
	var set map[string]any
	if !h.decode(w, r, &set) {
		return
	}
	ov, err := h.svc.PutSettings(r.Context(), actor(r), set)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsBody{Rev: ov.Rev, Settings: ov.Settings}, h.log)
}

// Object serves GET /api/v1/config/{kind}/{id}.
func (h *Config) Object(w http.ResponseWriter, r *http.Request) {
	o, err := h.svc.Object(r.Context(), r.PathValue("kind"), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	ov, err := h.svc.Overview(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, objectBody{Rev: ov.Rev, Object: o}, h.log)
}

// PutObject serves PUT /api/v1/config/{kind}/{id}; the body is the object in file form.
func (h *Config) PutObject(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if !h.decode(w, r, &body) {
		return
	}
	o, rev, err := h.svc.PutObject(r.Context(), actor(r), r.PathValue("kind"),
		r.PathValue("id"), body)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, objectBody{Rev: rev, Object: o}, h.log)
}

// batchBody is the body of PUT /api/v1/config.
type batchBody struct {
	Objects []configstore.Change `json:"objects"`
}

// PutObjects serves PUT /api/v1/config: {objects: [{kind, id, body}]} as one revision; a
// null body deletes the object. Answers like GET /api/v1/config.
func (h *Config) PutObjects(w http.ResponseWriter, r *http.Request) {
	var b batchBody
	if !h.decode(w, r, &b) {
		return
	}
	ov, err := h.svc.PutObjects(r.Context(), actor(r), b.Objects)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ov, h.log)
}

// probeBody is the body of POST /api/v1/config/probe: draft changes plus the device.
type probeBody struct {
	Objects []configstore.Change `json:"objects"`
	Device  string               `json:"device"`
}

// Probe serves POST /api/v1/config/probe: reads the device once with the draft applied.
// An unreachable device is a 200 with error set; an invalid draft is a 422.
func (h *Config) Probe(w http.ResponseWriter, r *http.Request) {
	var b probeBody
	if !h.decode(w, r, &b) {
		return
	}
	if b.Device == "" {
		fail(w, apiError{http.StatusBadRequest, "invalid_param", "device is required", nil},
			h.log)
		return
	}
	res, err := h.svc.Probe(r.Context(), actor(r), b.Objects, b.Device)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res, h.log)
}

// DeleteObject serves DELETE /api/v1/config/{kind}/{id}.
func (h *Config) DeleteObject(w http.ResponseWriter, r *http.Request) {
	_, err := h.svc.DeleteObject(r.Context(), actor(r), r.PathValue("kind"), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Export serves GET /api/v1/config/export: the configured layers as YAML, secrets masked.
func (h *Config) Export(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Export(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="config.yaml"`)
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(out); err != nil {
		h.log.Debug().Err(err).Msg("write export")
	}
}

// Profiles serves GET /api/v1/profiles.
func (h *Config) Profiles(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, profilesBody{Profiles: h.profiles.List()}, h.log)
}

func actor(r *http.Request) string {
	p, _ := auth.PrincipalFrom(r.Context()) // guarded by middleware.Admin
	return p.Actor()
}

// decode reads a JSON object of at most maxBody bytes.
func (h *Config) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	if err := dec.Decode(v); err != nil {
		fail(w, apiError{http.StatusBadRequest, "invalid_param", "invalid JSON body", nil},
			h.log)
		return false
	}
	return true
}

// writeError maps configstore errors onto the codes of spec 9.2.
func (h *Config) writeError(w http.ResponseWriter, err error) {
	var ve *configstore.ValidationError
	if errors.As(err, &ve) {
		details := make([]errorDetail, len(ve.Problems))
		for i, p := range ve.Problems {
			details[i] = errorDetail{Path: p.Path, Message: p.Message}
		}
		fail(w, apiError{
			http.StatusUnprocessableEntity, "validation_failed",
			"the configuration is invalid", details,
		}, h.log)
		return
	}
	fail(w, h.mapError(err), h.log)
}

func (h *Config) mapError(err error) apiError {
	switch {
	case errors.Is(err, configstore.ErrNotFound), errors.Is(err, configstore.ErrUnknownKind):
		return apiError{http.StatusNotFound, "not_found", "not found", nil}
	case errors.Is(err, configstore.ErrLocked):
		return apiError{
			http.StatusConflict, "locked_by_file", "defined in config.yaml or environment", nil,
		}
	case errors.Is(err, configstore.ErrConflict):
		return apiError{
			http.StatusConflict, "conflict", "the configuration was changed meanwhile", nil,
		}
	}
	h.log.Error().Err(err).Msg("config")
	return apiError{http.StatusInternalServerError, "internal", "the request failed", nil}
}
