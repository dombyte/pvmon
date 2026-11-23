package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/derive"
	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/service/mocks"
)

func TestGaugeSeries(t *testing.T) {
	t.Parallel()
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	d := mocks.NewMockGaugeDeriver(t)
	s, err := service.NewGauge(d, loc)
	require.NoError(t, err)
	day := time.Date(2026, 11, 19, 0, 0, 0, 0, loc)
	slot := day.Unix() / 900
	d.EXPECT().Gauge(mock.Anything, []string{"site/pv"}, day, day).Return([]derive.GaugeResult{{
		Name: "site/pv", Unit: "W",
		Points: []derive.GaugePoint{{Slot: slot, Avg: 1.234, Min: 1, Max: 2.005, Cover: 900}},
	}}, nil).Once()
	m, err := s.Series(context.Background(), []string{"site/pv"}, day, day)
	require.NoError(t, err)
	b, err := json.Marshal(m)
	require.NoError(t, err)
	assert.JSONEq(t, `{"series":[{"name":"site/pv","unit":"W","points":[`+
		`{"at":"2026-11-19T00:00:00+01:00","avg":1.23,"min":1,"max":2.01,"cover":900}]}]}`, string(b))

	d.EXPECT().Gauge(mock.Anything, mock.Anything, day, day).Return(nil, errors.New("boom")).Once()
	_, err = s.Series(context.Background(), []string{"x"}, day, day)
	require.Error(t, err)
	_, err = service.NewGauge(nil, nil)
	require.Error(t, err)
}
