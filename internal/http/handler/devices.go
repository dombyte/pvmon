package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/util"
)

// defaultStatusDays is the status range when start is omitted.
const defaultStatusDays = 90

// DevicesService is what the device handlers need.
type DevicesService interface {
	List() []service.DeviceInfo
	Get(id string) (service.DeviceDetail, error)
	Status(ctx context.Context, id string, from, to time.Time,
	) ([]service.StatusEventModel, error)
	Overview(ctx context.Context, from, to time.Time) ([]service.DeviceStatus, error)
}

// Devices serves GET /api/v1/devices, /api/v1/devices/{id} and its /status.
type Devices struct {
	svc   DevicesService
	clock util.Clock
	loc   *time.Location
	log   zerolog.Logger
}

// NewDevices builds the handlers.
func NewDevices(svc DevicesService, clock util.Clock, loc *time.Location, log zerolog.Logger,
) (*Devices, error) {
	if err := util.RequireAll("handler", util.D("service", svc), util.D("clock", clock),
		util.D("location", loc)); err != nil {
		return nil, err
	}
	return &Devices{svc: svc, clock: clock, loc: loc, log: log}, nil
}

// List writes every device.
func (h *Devices) List(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": h.svc.List()}, h.log)
}

// Get writes one device.
func (h *Devices) Get(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Get(r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d, h.log)
}

// Status writes the status changes of a device; start/end are RFC 3339 times or local
// dates (end inclusive), by default the last 90 days up to now.
func (h *Devices) Status(w http.ResponseWriter, r *http.Request) {
	from, to, details := h.statusRange(r.URL.Query().Get("start"), r.URL.Query().Get("end"))
	if len(details) > 0 {
		fail(w, apiError{
			http.StatusBadRequest, "invalid_param", "invalid query parameters",
			details,
		}, h.log)
		return
	}
	ev, err := h.svc.Status(r.Context(), r.PathValue("id"), from, to)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": ev}, h.log)
}

// Overview writes the generic level of every device over time (GET /api/v1/status);
// start/end as for Status.
func (h *Devices) Overview(w http.ResponseWriter, r *http.Request) {
	from, to, details := h.statusRange(r.URL.Query().Get("start"), r.URL.Query().Get("end"))
	if len(details) > 0 {
		fail(w, apiError{
			http.StatusBadRequest, "invalid_param", "invalid query parameters",
			details,
		}, h.log)
		return
	}
	devs, err := h.svc.Overview(r.Context(), from, to)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devs}, h.log)
}

func (h *Devices) statusRange(startS, endS string) (from, to time.Time, d []errorDetail) {
	to = h.clock.Now().In(h.loc)
	if endS != "" {
		t, err := h.instant(endS, true)
		if err != nil {
			d = append(d, errorDetail{"end", err.Error()})
		}
		to = t
	}
	from = to.AddDate(0, 0, -defaultStatusDays)
	if startS != "" {
		t, err := h.instant(startS, false)
		if err != nil {
			d = append(d, errorDetail{"start", err.Error()})
		}
		from = t
	}
	return from, to, d
}

// instant reads an RFC 3339 time or a local date (its midnight; the next one for an end).
func (h *Devices) instant(s string, end bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation(time.DateOnly, s, h.loc)
	if err != nil {
		return time.Time{}, errors.New("want YYYY-MM-DD or RFC 3339")
	}
	if end {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}

func (h *Devices) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		fail(w, apiError{http.StatusNotFound, "not_found", "unknown device", nil}, h.log)
	case errors.Is(err, service.ErrInvalidRange):
		fail(w, apiError{
			http.StatusBadRequest, "invalid_range", "end must not be before start",
			nil,
		}, h.log)
	default:
		h.log.Error().Err(err).Msg("devices")
		fail(w, apiError{
			http.StatusInternalServerError, "internal",
			"the device data is not available", nil,
		}, h.log)
	}
}
