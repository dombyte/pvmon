package derive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/derive/mocks"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

func TestGaugeCatalogue(t *testing.T) {
	t.Parallel()
	cat := GaugeCatalogue(buildSite(t, true, false))
	assert.Equal(t, []gaugeTerm{{"pv/power", 1}}, cat[GaugeSitePV].Terms)
	assert.Equal(t, "W", cat[GaugeSitePV].Unit)
	assert.Equal(t, []gaugeTerm{{"battery/soc", 1}}, cat[GaugeSiteSoC].Terms)
	assert.Contains(t, cat, "grid/power")
	assert.NotContains(t, cat, "pv/energy_out", "energy is no gauge")

	two := GaugeCatalogue(buildSiteWith(t, true, false, map[string]site.MeterSpec{
		"bat2": {Device: "wr1", Source: "battery", CapacityWh: 30000},
	}))
	assert.Equal(t, []gaugeTerm{{"bat2/soc", 0.75}, {"battery/soc", 0.25}}, two[GaugeSiteSoC].Terms)
	assert.Equal(t, []gaugeTerm{{"bat2/soc", 0.5}, {"battery/soc", 0.5}},
		socTerms([]*site.Meter{{ID: "bat2"}, {ID: "battery", CapacityWh: 1}}))

	none := GaugeCatalogue(buildSite(t, false, false))
	assert.NotContains(t, none, GaugeSiteSoC)
}

func TestGaugeCombine(t *testing.T) {
	t.Parallel()
	f := GaugeFormula{Terms: []gaugeTerm{{"a", 1}, {"b", 0.5}}}
	got := f.Combine(map[string][]storage.GaugeBucket{
		"a": {{Slot: 2, Avg: 10, Min: 5, Max: 20, Cover: 900}, {Slot: 1, Avg: 1, Min: 1, Max: 1, Cover: 600}},
		"b": {{Slot: 1, Avg: 4, Min: 2, Max: 8, Cover: 900}},
	})
	assert.Equal(t, []GaugePoint{{Slot: 1, Avg: 3, Min: 2, Max: 5, Cover: 600}}, got,
		"slot 2 lacks b: no data, never a partial sum")
}

func TestGaugeService(t *testing.T) {
	t.Parallel()
	loc := berlin(t)
	s := buildSite(t, true, false)
	rd := mocks.NewMockReader(t)
	svc, err := New(s, rd, mocks.NewMockCache(t), clocktest.New(time.Now()), loc)
	require.NoError(t, err)
	assert.Contains(t, svc.GaugeNames(), GaugeSitePV)
	ctx := context.Background()
	day := time.Date(2026, 11, 19, 0, 0, 0, 0, loc)
	first := Slot(day)

	rd.EXPECT().SeriesID(mock.Anything, storage.SeriesKey{Meter: "pv", Channel: "power"}).
		Return(7, nil).Once()
	rd.EXPECT().SeriesID(mock.Anything, storage.SeriesKey{Meter: "battery", Channel: "soc"}).
		Return(0, storage.ErrNotFound).Once()
	rd.EXPECT().GaugeBuckets(mock.Anything, int64(7), first, first+96).
		Return([]storage.GaugeBucket{{Slot: first, Avg: 100, Min: 50, Max: 150, Cover: 900}}, nil).Once()
	res, err := svc.Gauge(ctx, []string{GaugeSitePV, "pv/power", GaugeSiteSoC}, day, day)
	require.NoError(t, err)
	require.Len(t, res, 3)
	assert.Equal(t, "W", res[0].Unit)
	assert.Equal(t, []GaugePoint{{Slot: first, Avg: 100, Min: 50, Max: 150, Cover: 900}}, res[0].Points)
	assert.Equal(t, res[0].Points, res[1].Points)
	assert.Empty(t, res[2].Points, "never stored: no data")

	_, err = svc.Gauge(ctx, []string{"nope"}, day, day)
	var unknown *UnknownSeriesError
	require.ErrorAs(t, err, &unknown)
	_, err = svc.Gauge(ctx, []string{GaugeSitePV}, day, day.AddDate(0, 0, -1))
	require.ErrorIs(t, err, ErrInvalidRange)

	boom := errors.New("boom")
	rd.EXPECT().SeriesID(mock.Anything, mock.Anything).Return(7, nil).Once()
	rd.EXPECT().GaugeBuckets(mock.Anything, int64(7), first, first+96).Return(nil, boom).Once()
	_, err = svc.Gauge(ctx, []string{GaugeSitePV}, day, day)
	require.ErrorIs(t, err, boom)
	rd.EXPECT().SeriesID(mock.Anything, mock.Anything).Return(0, boom).Once()
	_, err = svc.Gauge(ctx, []string{GaugeSitePV}, day, day)
	require.ErrorIs(t, err, boom)
}
