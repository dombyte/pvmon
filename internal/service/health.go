// Package service holds the read services behind the HTTP handlers.
package service

import (
	"net/http"
	"sync"
	"time"

	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/util"
)

// SnapshotSource is the supervisor's health snapshot.
type SnapshotSource interface {
	Snapshot() health.Snapshot
}

// HealthSettings configures the /health semantics (spec 8.2).
type HealthSettings struct {
	Sweep        time.Duration
	OfflineAfter time.Duration   // status.offline_after
	Critical     map[string]bool // devices that provide a critical meter
}

// Health statuses.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
	StatusFailed   = "failed"
)

// staleSweeps: a snapshot older than this many sweeps is a failure.
const staleSweeps = 3

// HealthReport is the body of GET /health.
type HealthReport struct {
	Status     string                   `json:"status"`
	Components []health.ComponentStatus `json:"components"`
	Devices    map[string]string        `json:"devices"`
}

// Health evaluates the supervisor snapshot; it never blocks on a component.
type Health struct {
	src   SnapshotSource
	clock util.Clock
	s     HealthSettings

	mu      sync.Mutex
	offline map[string]time.Time // device → offline since
}

// NewHealth builds the service.
func NewHealth(src SnapshotSource, clock util.Clock, s HealthSettings) (*Health, error) {
	if err := util.RequireAll("health service", util.D("source", src),
		util.D("clock", clock)); err != nil {
		return nil, err
	}
	return &Health{src: src, clock: clock, s: s, offline: map[string]time.Time{}}, nil
}

// Report returns the health report and its HTTP status code.
func (h *Health) Report() (HealthReport, int) {
	snap := h.src.Snapshot()
	now := h.clock.Now()
	rep := HealthReport{Status: StatusOK, Components: snap.Components, Devices: snap.Devices}
	degraded := h.trackOffline(snap.Devices, now)
	for _, c := range snap.Components {
		degraded = degraded || c.State != health.Healthy.String()
	}
	if degraded {
		rep.Status = StatusDegraded
	}
	if snap.Fatal || h.criticalDown(now) || now.Sub(snap.At) > staleSweeps*h.s.Sweep {
		rep.Status = StatusFailed
		return rep, http.StatusServiceUnavailable
	}
	return rep, http.StatusOK
}

// trackOffline records since when devices are offline and reports whether any is.
func (h *Health) trackOffline(devices map[string]string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	offline := false
	for id, state := range devices {
		if state != health.DeviceOffline {
			delete(h.offline, id)
			continue
		}
		offline = true
		if _, ok := h.offline[id]; !ok {
			h.offline[id] = now
		}
	}
	return offline
}

func (h *Health) criticalDown(now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, since := range h.offline {
		if h.s.Critical[id] && now.Sub(since) > h.s.OfflineAfter {
			return true
		}
	}
	return false
}
