package handler_test

import (
	"errors"
	"fmt"
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
)

func gaugeHandler(t *testing.T) (*handler.Gauge, *mocks.MockGaugeService) {
	t.Helper()
	svc := mocks.NewMockGaugeService(t)
	h, err := handler.NewGauge(svc, berlin, zerolog.Nop())
	require.NoError(t, err)
	return h, svc
}

func TestGaugeOK(t *testing.T) {
	t.Parallel()
	h, svc := gaugeHandler(t)
	day := time.Date(2026, 11, 18, 0, 0, 0, 0, berlin)
	svc.EXPECT().Series(mock.Anything, []string{"site/pv", "site/soc"}, day, day).
		Return(service.GaugeModel{Series: []service.GaugeSeries{{Name: "site/pv"}}}, nil)
	rec := get(h, "/api/v1/gauge?series=site/pv,site/soc&from=2026-11-18&to=2026-11-18")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"site/pv"`)
	_, err := handler.NewGauge(nil, nil, zerolog.Nop())
	require.Error(t, err)
}

func TestGaugeErrors(t *testing.T) {
	t.Parallel()
	h, svc := gaugeHandler(t)
	rec := get(h, "/api/v1/gauge?from=x")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	for _, k := range []string{`"series"`, `"from"`, `"to"`} {
		assert.Contains(t, rec.Body.String(), k)
	}
	cases := map[error]string{
		&service.UnknownSeriesError{Names: []string{"x"}}: "unknown_series",
		service.ErrInvalidRange:                           "invalid_range",
		service.ErrRangeTooLong:                           "range_too_long",
		errors.New("boom"):                                "internal",
	}
	for err, code := range cases {
		svc.EXPECT().Series(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(service.GaugeModel{}, fmt.Errorf("w: %w", err)).Once()
		rec := get(h, "/api/v1/gauge?series=a&from=2026-11-18&to=2026-11-18")
		assert.Contains(t, rec.Body.String(), `"code":"`+code+`"`)
	}
}
