package handler_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

var berlin = func() *time.Location {
	l, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic(err)
	}
	return l
}()

func energyHandler(t *testing.T) (*handler.Energy, *mocks.MockEnergyService) {
	t.Helper()
	svc := mocks.NewMockEnergyService(t)
	clk := clocktest.New(time.Date(2026, 11, 19, 23, 30, 0, 0, time.UTC)) // 00:30 on the 20th
	h, err := handler.NewEnergy(svc, clk, berlin, zerolog.Nop())
	require.NoError(t, err)
	return h, svc
}

func get(h http.Handler, url string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func TestEnergyDeps(t *testing.T) {
	t.Parallel()
	_, err := handler.NewEnergy(nil, nil, nil, zerolog.Nop())
	require.Error(t, err)
}

func TestEnergyOK(t *testing.T) {
	t.Parallel()
	h, svc := energyHandler(t)
	day := time.Date(2026, 11, 18, 0, 0, 0, 0, berlin)
	svc.EXPECT().Series(mock.Anything, []string{"site/pv", "site/load"}, "day", day,
		day.AddDate(0, 0, 1)).Return(service.EnergyModel{}, nil)
	rec := get(h, "/api/v1/energy?series=site/pv,%20site/load,site/pv,&level=day"+
		"&from=2026-11-18&to=2026-11-19")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"series":null}`, rec.Body.String())
}

func TestEnergyRFC3339AndTotalDefaults(t *testing.T) {
	t.Parallel()
	h, svc := energyHandler(t)
	today := time.Date(2026, 11, 20, 0, 30, 0, 0, berlin)
	svc.EXPECT().Series(mock.Anything, []string{"a"}, "total", mock.MatchedBy(today.Equal),
		mock.MatchedBy(today.Equal)).Return(service.EnergyModel{}, nil)
	assert.Equal(t, http.StatusOK, get(h, "/api/v1/energy?series=a&level=total").Code)

	// 23:30 UTC on the 18th is the 19th in Berlin.
	want := time.Date(2026, 11, 19, 0, 30, 0, 0, berlin)
	svc.EXPECT().Series(mock.Anything, []string{"a"}, "day", mock.MatchedBy(want.Equal),
		mock.MatchedBy(want.Equal)).Return(service.EnergyModel{}, nil)
	url := "/api/v1/energy?series=a&level=day&from=2026-11-18T23:30:00Z&to=2026-11-18T23:30:00Z"
	assert.Equal(t, http.StatusOK, get(h, url).Code)
}

func TestEnergyInvalidParams(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := range 65 {
		sb.WriteString("s" + strconv.Itoa(i) + ",")
	}
	many := sb.String()
	for name, url := range map[string]string{
		"no series":  "/api/v1/energy?level=day&from=2026-11-18&to=2026-11-18",
		"no level":   "/api/v1/energy?series=a&from=2026-11-18&to=2026-11-18",
		"no from":    "/api/v1/energy?series=a&level=day&to=2026-11-18",
		"bad date":   "/api/v1/energy?series=a&level=day&from=18.11.2026&to=2026-11-18",
		"bad to":     "/api/v1/energy?series=a&level=day&from=2026-11-18&to=x",
		"too many":   "/api/v1/energy?level=day&from=2026-11-18&to=2026-11-18&series=" + many,
		"empty list": "/api/v1/energy?series=,,&level=total",
	} {
		h, _ := energyHandler(t)
		rec := get(h, url)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `"code":"invalid_param"`, name)
		assert.Contains(t, rec.Body.String(), `"details":[{"path":`, name)
	}
}

func TestEnergyServiceErrors(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"level":   {service.ErrInvalidLevel, 400, "invalid_param"},
		"range":   {service.ErrInvalidRange, 400, "invalid_range"},
		"long":    {service.ErrRangeTooLong, 400, "range_too_long"},
		"unknown": {&service.UnknownSeriesError{Names: []string{"x", "y"}}, 400, "unknown_series"},
		"other":   {errors.New("boom"), 500, "internal"},
	} {
		h, svc := energyHandler(t)
		svc.EXPECT().Series(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
			mock.Anything).Return(service.EnergyModel{}, tc.err)
		rec := get(h, "/api/v1/energy?series=a&level=day&from=2026-11-18&to=2026-11-18")
		assert.Equal(t, tc.status, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `"code":"`+tc.code+`"`, name)
		if name == "unknown" {
			assert.Contains(t, rec.Body.String(), "unknown series y")
		}
		if name == "other" {
			assert.NotContains(t, rec.Body.String(), "boom")
		}
	}
}
