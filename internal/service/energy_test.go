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

func ptr(f float64) *float64 { return &f }

func TestEnergySeries(t *testing.T) {
	t.Parallel()
	d := mocks.NewMockEnergyDeriver(t)
	day := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	d.EXPECT().Energy(mock.Anything, []string{"site/pv", "site/self"}, derive.LevelDay, day, day).
		Return([]derive.Result{
			{Name: "site/pv", Values: []derive.PeriodValue{
				{Period: "2026-11-18", Value: derive.Value{Value: ptr(6213.4567), Complete: true}},
				{Period: "2026-11-19", Value: derive.Value{MissingDays: 1}},
			}},
			{Name: "site/self", Ratio: true, Values: []derive.PeriodValue{
				{Period: "2026-11-18", Value: derive.Value{Value: ptr(55.555), Estimated: true}},
				{Period: "2026-11-19"},
			}},
		}, nil)
	svc, err := service.NewEnergy(d)
	require.NoError(t, err)
	m, err := svc.Series(context.Background(), []string{"site/pv", "site/self"}, "day", day, day)
	require.NoError(t, err)
	b, err := json.Marshal(m)
	require.NoError(t, err)
	assert.JSONEq(t, `{"series":[
	 {"name":"site/pv","unit":"Wh","values":[
	  {"period":"2026-11-18","wh":6213.46,"complete":true,"estimated":false,"missing_days":0},
	  {"period":"2026-11-19","wh":null,"complete":false,"estimated":false,"missing_days":1}]},
	 {"name":"site/self","unit":"%","values":[
	  {"period":"2026-11-18","pct":55.56,"complete":false,"estimated":true,"missing_days":0},
	  {"period":"2026-11-19","pct":null,"complete":false,"estimated":false,"missing_days":0}]}]}`,
		string(b))
}

func TestEnergyErrorsAndDeps(t *testing.T) {
	t.Parallel()
	_, err := service.NewEnergy(nil)
	require.Error(t, err)
	d := mocks.NewMockEnergyDeriver(t)
	d.EXPECT().Energy(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, derive.ErrRangeTooLong)
	svc, err := service.NewEnergy(d)
	require.NoError(t, err)
	_, err = svc.Series(context.Background(), []string{"a"}, "day", time.Time{}, time.Time{})
	assert.True(t, errors.Is(err, service.ErrRangeTooLong))
}
