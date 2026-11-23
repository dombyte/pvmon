// Package handler holds the HTTP handlers; they contain no business logic and call
// services only.
package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/buildinfo"
	"github.com/dombyte/pvmon/internal/service"
)

// HealthService is what the health handler needs.
type HealthService interface {
	Report() (service.HealthReport, int)
}

// Health serves GET /health.
type Health struct {
	svc HealthService
	log zerolog.Logger
}

// NewHealth builds the handler.
func NewHealth(svc HealthService, log zerolog.Logger) *Health {
	return &Health{svc: svc, log: log}
}

// ServeHTTP writes the health report with 200 or 503.
func (h *Health) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	rep, code := h.svc.Report()
	writeJSON(w, code, rep, h.log)
}

// versionBody is the body of GET /api/v1/version (spec 9.1).
type versionBody struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	Go        string `json:"go"`
}

// Version serves GET /api/v1/version.
type Version struct {
	body versionBody
	log  zerolog.Logger
}

// NewVersion builds the handler from the build info.
func NewVersion(b buildinfo.Info, log zerolog.Logger) *Version {
	return &Version{body: versionBody{
		Version: b.Version, Commit: b.Commit,
		BuildDate: b.BuildDate, Go: b.GoVersion,
	}, log: log}
}

// ServeHTTP writes the version.
func (v *Version) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, v.body, v.log)
}

// SiteService is what the site handler needs.
type SiteService interface {
	Model(ctx context.Context) (service.SiteModel, error)
}

// Site serves GET /api/v1/site.
type Site struct {
	svc SiteService
	log zerolog.Logger
}

// NewSite builds the handler.
func NewSite(svc SiteService, log zerolog.Logger) *Site {
	return &Site{svc: svc, log: log}
}

// ServeHTTP writes the site model.
func (h *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m, err := h.svc.Model(r.Context())
	if err != nil {
		h.log.Error().Err(err).Msg("site model")
		writeJSON(w, http.StatusInternalServerError, errorBody{
			Error: http.StatusText(http.StatusInternalServerError), Code: "internal",
			Message: "the site model is not available",
		}, h.log)
		return
	}
	writeJSON(w, http.StatusOK, m, h.log)
}

// errorBody is the one error shape of the API (spec 9.2).
type errorBody struct {
	Error   string        `json:"error"`
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details []errorDetail `json:"details,omitempty"`
}

// errorDetail names the offending field or item of an error (spec 9.2).
type errorDetail struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, code int, body any, log zerolog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Debug().Err(err).Msg("write response")
	}
}

// apiError is one error answer (spec 9.2).
type apiError struct {
	status  int
	code    string
	msg     string
	details []errorDetail
}

// fail writes the error shape of spec 9.2.
func fail(w http.ResponseWriter, e apiError, log zerolog.Logger) {
	writeJSON(w, e.status, errorBody{
		Error: http.StatusText(e.status), Code: e.code, Message: e.msg, Details: e.details,
	}, log)
}
