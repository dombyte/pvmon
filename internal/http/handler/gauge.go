package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/util"
)

// GaugeService is what the gauge handler needs.
type GaugeService interface {
	Series(ctx context.Context, names []string, from, to time.Time) (service.GaugeModel, error)
}

// Gauge serves GET /api/v1/gauge.
type Gauge struct {
	svc GaugeService
	loc *time.Location
	log zerolog.Logger
}

// NewGauge builds the handler; loc is the site's location for local dates.
func NewGauge(svc GaugeService, loc *time.Location, log zerolog.Logger) (*Gauge, error) {
	err := util.RequireAll("handler", util.D("service", svc), util.D("location", loc))
	if err != nil {
		return nil, err
	}
	return &Gauge{svc: svc, loc: loc, log: log}, nil
}

// ServeHTTP parses the query and writes the slots.
func (h *Gauge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	names := parseSeries(q.Get("series"))
	var details []errorDetail
	if len(names) == 0 || len(names) > maxEnergySeries {
		details = append(details, errorDetail{
			"series", fmt.Sprintf("1 to %d comma separated series names required", maxEnergySeries),
		})
	}
	from, err := parseLocalDate(q.Get("from"), h.loc)
	if err != nil {
		details = append(details, errorDetail{"from", err.Error()})
	}
	to, err := parseLocalDate(q.Get("to"), h.loc)
	if err != nil {
		details = append(details, errorDetail{"to", err.Error()})
	}
	if len(details) > 0 {
		h.fail(w, http.StatusBadRequest, "invalid_param", "invalid query parameters", details)
		return
	}
	m, err := h.svc.Series(r.Context(), names, from, to)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m, h.log)
}

func (h *Gauge) writeError(w http.ResponseWriter, err error) {
	var unknown *service.UnknownSeriesError
	switch {
	case errors.As(err, &unknown):
		h.fail(w, http.StatusBadRequest, "unknown_series", "unknown series",
			seriesErrorDetails(unknown.Names))
	case errors.Is(err, service.ErrInvalidRange):
		h.fail(w, http.StatusBadRequest, "invalid_range", "to must not be before from", nil)
	case errors.Is(err, service.ErrRangeTooLong):
		h.fail(w, http.StatusBadRequest, "range_too_long", "the range is too long", nil)
	default:
		h.log.Error().Err(err).Msg("gauge")
		h.fail(w, http.StatusInternalServerError, "internal", "the gauge data is not available",
			nil)
	}
}

func (h *Gauge) fail(w http.ResponseWriter, status int, code, msg string, d []errorDetail) {
	writeJSON(w, status, errorBody{
		Error: http.StatusText(status), Code: code, Message: msg, Details: d,
	}, h.log)
}
