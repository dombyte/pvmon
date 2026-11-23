package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/util"
)

// maxEnergySeries bounds the series parameter (spec 9.1).
const maxEnergySeries = 64

// EnergyService is what the energy handler needs.
type EnergyService interface {
	Series(ctx context.Context, names []string, level string, from, to time.Time,
	) (service.EnergyModel, error)
}

// Energy serves GET /api/v1/energy.
type Energy struct {
	svc   EnergyService
	clock util.Clock
	loc   *time.Location
	log   zerolog.Logger
}

// NewEnergy builds the handler; loc is the site's location for local dates.
func NewEnergy(svc EnergyService, clock util.Clock, loc *time.Location, log zerolog.Logger,
) (*Energy, error) {
	if err := util.RequireAll("handler", util.D("service", svc), util.D("clock", clock),
		util.D("location", loc)); err != nil {
		return nil, err
	}
	return &Energy{svc: svc, clock: clock, loc: loc, log: log}, nil
}

// ServeHTTP parses the query and writes the periods.
func (h *Energy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	names, level := parseSeries(q.Get("series")), q.Get("level")
	from, to, details := h.parseQuery(names, level, q.Get("from"), q.Get("to"))
	if len(details) > 0 {
		h.fail(w, http.StatusBadRequest, "invalid_param", "invalid query parameters", details)
		return
	}
	m, err := h.svc.Series(r.Context(), names, level, from, to)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m, h.log)
}

// parseQuery validates everything and reports every problem at once.
func (h *Energy) parseQuery(names []string, level, fromS, toS string,
) (from, to time.Time, details []errorDetail) {
	if len(names) == 0 || len(names) > maxEnergySeries {
		details = append(details, errorDetail{
			"series",
			fmt.Sprintf("1 to %d comma separated series names required", maxEnergySeries),
		})
	}
	if level == "" {
		details = append(details, errorDetail{"level", "required"})
	}
	from, fromErr := h.parseDate(fromS, level)
	to, toErr := h.parseDate(toS, level)
	if fromErr != nil {
		details = append(details, errorDetail{"from", fromErr.Error()})
	}
	if toErr != nil {
		details = append(details, errorDetail{"to", toErr.Error()})
	}
	return from, to, details
}

// parseDate reads a local date or an RFC 3339 time (reduced to its local date); an empty
// value is today for level=total and an error otherwise.
func (h *Energy) parseDate(s, level string) (time.Time, error) {
	if s == "" {
		if level == "total" {
			return h.clock.Now().In(h.loc), nil
		}
		return time.Time{}, errors.New("required")
	}
	return parseLocalDate(s, h.loc)
}

// parseLocalDate reads a local date or an RFC 3339 time (reduced to its local date).
func parseLocalDate(s string, l *time.Location) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("required")
	}
	if t, err := time.ParseInLocation(time.DateOnly, s, l); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, errors.New("want YYYY-MM-DD or RFC 3339")
	}
	return t.In(l), nil
}

func (h *Energy) writeError(w http.ResponseWriter, err error) {
	var unknown *service.UnknownSeriesError
	switch {
	case errors.As(err, &unknown):
		h.fail(w, http.StatusBadRequest, "unknown_series", "unknown series",
			seriesErrorDetails(unknown.Names))
	case errors.Is(err, service.ErrInvalidLevel):
		h.fail(w, http.StatusBadRequest, "invalid_param", "invalid level",
			[]errorDetail{{"level", "want bucket, day, month, year or total"}})
	case errors.Is(err, service.ErrInvalidRange):
		h.fail(w, http.StatusBadRequest, "invalid_range", "to must not be before from", nil)
	case errors.Is(err, service.ErrRangeTooLong):
		h.fail(w, http.StatusBadRequest, "range_too_long", "the range is too long", nil)
	default:
		h.log.Error().Err(err).Msg("energy")
		h.fail(w, http.StatusInternalServerError, "internal", "the energy data is not available",
			nil)
	}
}

// seriesErrorDetails lists unknown series names as error details.
func seriesErrorDetails(names []string) []errorDetail {
	d := make([]errorDetail, len(names))
	for i, n := range names {
		d[i] = errorDetail{"series", "unknown series " + n}
	}
	return d
}

func (h *Energy) fail(w http.ResponseWriter, status int, code, msg string, d []errorDetail) {
	writeJSON(w, status, errorBody{
		Error: http.StatusText(status), Code: code, Message: msg, Details: d,
	}, h.log)
}

// parseSeries splits, trims and deduplicates the comma separated names, keeping order.
func parseSeries(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range strings.Split(s, ",") {
		n = strings.TrimSpace(n)
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}
