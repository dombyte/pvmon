package service_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/service/mocks"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

func TestHealthReport(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	clock := clocktest.New(t0)
	src := mocks.NewMockSnapshotSource(t)
	h, err := service.NewHealth(src, clock, service.HealthSettings{
		Sweep:        5 * time.Second,
		OfflineAfter: 15 * time.Minute, Critical: map[string]bool{"meter": true},
	})
	require.NoError(t, err)
	healthy := []health.ComponentStatus{{Name: "c", State: "healthy"}}
	snap := func(devs map[string]string, comps []health.ComponentStatus, fatal bool) {
		src.EXPECT().Snapshot().Return(health.Snapshot{
			At: clock.Now(), Devices: devs,
			Components: comps, Fatal: fatal,
		}).Once()
	}
	check := func(status string, code int) {
		t.Helper()
		rep, c := h.Report()
		assert.Equal(t, status, rep.Status)
		assert.Equal(t, code, c)
	}
	snap(map[string]string{"meter": "online"}, healthy, false)
	check(service.StatusOK, http.StatusOK)
	snap(map[string]string{"meter": "online"}, []health.ComponentStatus{{State: "recovering"}},
		false)
	check(service.StatusDegraded, http.StatusOK)
	snap(map[string]string{"meter": "offline", "goe": "offline"}, healthy, false)
	check(service.StatusDegraded, http.StatusOK)
	clock.Advance(16 * time.Minute)
	snap(map[string]string{"meter": "offline"}, healthy, false)
	check(service.StatusFailed, http.StatusServiceUnavailable)
	snap(map[string]string{"meter": "online"}, healthy, true)
	check(service.StatusFailed, http.StatusServiceUnavailable)
	src.EXPECT().Snapshot().Return(health.Snapshot{At: clock.Now().Add(-time.Minute)}).Once()
	check(service.StatusFailed, http.StatusServiceUnavailable)

	_, err = service.NewHealth(nil, clock, service.HealthSettings{})
	require.Error(t, err)
}
