package handler_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/http/handler"
	"github.com/dombyte/pvmon/internal/http/handler/mocks"
	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

func deviceMux(t *testing.T) (*http.ServeMux, *mocks.MockDevicesService) {
	t.Helper()
	svc := mocks.NewMockDevicesService(t)
	clk := clocktest.New(time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	h, err := handler.NewDevices(svc, clk, berlin, zerolog.Nop())
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /d", h.List)
	mux.HandleFunc("GET /d/{id}", h.Get)
	mux.HandleFunc("GET /d/{id}/status", h.Status)
	mux.HandleFunc("GET /s", h.Overview)
	return mux, svc
}

func TestDevicesHandlers(t *testing.T) {
	t.Parallel()
	mux, svc := deviceMux(t)
	svc.EXPECT().List().Return([]service.DeviceInfo{{ID: "wr1"}})
	assert.Contains(t, get(mux, "/d").Body.String(), `"devices":[{"id":"wr1"`)

	svc.EXPECT().Get("wr1").Return(service.DeviceDetail{DeviceInfo: service.DeviceInfo{ID: "wr1"}}, nil)
	svc.EXPECT().Get("x").Return(service.DeviceDetail{}, service.ErrNotFound)
	assert.Equal(t, http.StatusOK, get(mux, "/d/wr1").Code)
	assert.Equal(t, http.StatusNotFound, get(mux, "/d/x").Code)

	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	svc.EXPECT().Status(mock.Anything, "wr1", now.In(berlin).AddDate(0, 0, -90), now.In(berlin)).
		Return([]service.StatusEventModel{{Register: "operating"}}, nil).Once()
	assert.Contains(t, get(mux, "/d/wr1/status").Body.String(), `"register":"operating"`)

	day := time.Date(2026, 11, 18, 0, 0, 0, 0, berlin)
	svc.EXPECT().Status(mock.Anything, "wr1", day, day.AddDate(0, 0, 1)).Return(nil, nil).Once()
	assert.Equal(t, http.StatusOK, get(mux, "/d/wr1/status?start=2026-11-18&end=2026-11-18").Code)
	rfc := time.Date(2026, 11, 18, 6, 0, 0, 0, time.UTC)
	svc.EXPECT().Status(mock.Anything, "wr1", rfc, rfc).Return(nil, service.ErrInvalidRange).Once()
	rec := get(mux, "/d/wr1/status?start=2026-11-18T06:00:00Z&end=2026-11-18T06:00:00Z")
	assert.Contains(t, rec.Body.String(), "invalid_range")

	rec = get(mux, "/d/wr1/status?start=x&end=y")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"start"`)
	assert.Contains(t, rec.Body.String(), `"end"`)

	svc.EXPECT().Status(mock.Anything, "wr1", mock.Anything, mock.Anything).
		Return(nil, errors.New("boom")).Once()
	assert.Equal(t, http.StatusInternalServerError, get(mux, "/d/wr1/status").Code)

	_, err := handler.NewDevices(nil, nil, nil, zerolog.Nop())
	require.Error(t, err)
}

func TestDevicesOverviewHandler(t *testing.T) {
	t.Parallel()
	mux, svc := deviceMux(t)
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, berlin)
	svc.EXPECT().Overview(mock.Anything, day, day.AddDate(0, 0, 19)).Return(
		[]service.DeviceStatus{{ID: "wr1", Changes: []service.LevelChange{{Level: "ok"}}}},
		nil).Once()
	rec := get(mux, "/s?start=2026-11-01&end=2026-11-19")
	assert.Contains(t, rec.Body.String(), `"devices":[{"id":"wr1","changes":[{"at"`)
	assert.Equal(t, http.StatusBadRequest, get(mux, "/s?start=x").Code)
	svc.EXPECT().Overview(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("boom")).Once()
	assert.Equal(t, http.StatusInternalServerError, get(mux, "/s").Code)
}
