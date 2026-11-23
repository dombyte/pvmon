package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/service/mocks"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/profiles"
)

func loadProfile(t *testing.T, id string) *profile.Loaded {
	t.Helper()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	p, err := l.Load(id, ".")
	require.NoError(t, err)
	return p
}

// devSite is the setup R1 + R3 + R4: Solis hybrid, Shelly grid meter, go-e and WARP.
func devSite(t *testing.T) *site.Site {
	t.Helper()
	s, err := site.Build(site.Spec{
		Name: "Home",
		Connections: map[string]site.ConnectionSpec{
			"lan-solis": {Address: "tcp://10.0.0.20", Timeout: 2 * time.Second},
		},
		Devices: map[string]site.DeviceSpec{
			"wr1": {
				Connection: "lan-solis", Unit: 1, Profile: loadProfile(t, "solis/s6-hybrid"),
				Interval: 5 * time.Second, Rollover: "23:59",
			},
			"meter": {
				Address: "http://10.0.0.31", Profile: loadProfile(t, "shelly/pro-3em"),
				Interval: 2 * time.Second, Timeout: time.Second,
			},
			"warp": {
				Address: "http://10.0.0.41", Profile: loadProfile(t, "tinkerforge/warp3"),
				User: "admin", Password: "s3cret-pw",
			},
		},
		Meters: map[string]site.MeterSpec{
			"pv":      {Device: "wr1", Source: "pv", KWp: 9.8},
			"battery": {Device: "wr1", Source: "battery", CapacityWh: 10000},
			"backup":  {Device: "wr1", Source: "backup"},
			"grid":    {Device: "meter", Source: "total"},
			"wb-2":    {Device: "warp", Source: "charger", Label: "Drive"},
			"car":     {Device: "warp", Source: "charger", Parent: "wb-2", Category: "other"},
		},
	})
	require.NoError(t, err)
	return s
}

func newSiteService(t *testing.T, first *mocks.MockFirstSlotSource,
	snap *mocks.MockSnapshotSource,
) *service.Site {
	t.Helper()
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	svc, err := service.NewSite(devSite(t), berlin, first, snap)
	require.NoError(t, err)
	return svc
}

func TestSiteModel(t *testing.T) {
	t.Parallel()
	first, snap := mocks.NewMockFirstSlotSource(t), mocks.NewMockSnapshotSource(t)
	// 2026-02-28 23:30 UTC is already 2026-03-01 in Berlin.
	slot := time.Date(2026, 2, 28, 23, 30, 0, 0, time.UTC).Unix() / 900
	first.EXPECT().FirstSlot(mock.Anything).Return(slot, true, nil)
	snap.EXPECT().Snapshot().Return(health.Snapshot{
		Devices: map[string]string{"wr1": health.DeviceOnline, "meter": health.DeviceOffline},
	})
	m, err := newSiteService(t, first, snap).Model(context.Background())
	require.NoError(t, err)
	body, err := json.Marshal(m)
	require.NoError(t, err)

	assert.NotContains(t, string(body), "s3cret-pw")
	assert.NotContains(t, string(body), "10.0.0")
	assert.NotContains(t, string(body), "admin")
	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "Home", got["name"])
	assert.Equal(t, "Europe/Berlin", got["tz"])
	assert.InDelta(t, 0, got["config_rev"], 0)
	assert.Equal(t, "2026-03-01", got["data_since"])
	assert.Equal(t, map[string]any{
		"pv": 1.0, "battery": 1.0, "grid": true, "backup": true,
		"loadpoints": map[string]any{"wallbox": 1.0, "heatpump": 0.0, "heater": 0.0, "other": 0.0},
	}, got["capabilities"])
	assert.Equal(t, []string{
		"site/pv", "site/grid_in", "site/grid_out", "site/battery_in", "site/battery_out",
		"site/soc", "site/consumption", "site/house_rest", "site/loadpoints/wallbox",
	}, m.SiteSeries)

	require.Len(t, m.Devices, 3)
	assert.Equal(t, []service.DeviceModel{
		{ID: "meter", Profile: "shelly/pro-3em", Verified: m.Devices[0].Verified, State: "offline"},
		{
			ID: "warp", Profile: "tinkerforge/warp3", Verified: m.Devices[1].Verified,
			State: "unconfigured",
		},
		{ID: "wr1", Profile: "solis/s6-hybrid", Verified: m.Devices[2].Verified, State: "online"},
	}, m.Devices)

	ids := make([]string, 0, len(m.Meters))
	byID := map[string]service.MeterModel{}
	for _, mm := range m.Meters {
		ids = append(ids, mm.ID)
		byID[mm.ID] = mm
	}
	assert.Equal(t, []string{"backup", "battery", "car", "grid", "pv", "wb-2"}, ids)
	bat := byID["battery"]
	assert.Equal(t, "battery", bat.Role)
	assert.Empty(t, bat.Category)
	assert.Empty(t, bat.Parent, "only consumers and pv meters have a parent")
	assert.InDelta(t, 10000, bat.CapacityWh, 0)
	assert.Contains(t, bat.Series, "battery/soc")
	assert.IsIncreasing(t, bat.Series)
	assert.InDelta(t, 9.8, byID["pv"].KWp, 0)
	assert.Equal(t, "Drive", byID["wb-2"].Label)
	assert.Equal(t, "site", byID["wb-2"].Parent)
	assert.Equal(t, "wb-2", byID["car"].Parent)
	assert.Equal(t, "other", byID["car"].Category)
	assert.NotContains(t, string(body), `"capacity_wh":0`)
}

func TestSiteModelNoData(t *testing.T) {
	t.Parallel()
	first, snap := mocks.NewMockFirstSlotSource(t), mocks.NewMockSnapshotSource(t)
	first.EXPECT().FirstSlot(mock.Anything).Return(0, false, nil)
	snap.EXPECT().Snapshot().Return(health.Snapshot{})
	m, err := newSiteService(t, first, snap).Model(context.Background())
	require.NoError(t, err)
	assert.Nil(t, m.DataSince)
	body, err := json.Marshal(m)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"data_since":null`)
}

func TestSiteModelErrorsAndDeps(t *testing.T) {
	t.Parallel()
	first, snap := mocks.NewMockFirstSlotSource(t), mocks.NewMockSnapshotSource(t)
	first.EXPECT().FirstSlot(mock.Anything).Return(0, false, errors.New("db"))
	_, err := newSiteService(t, first, snap).Model(context.Background())
	require.ErrorContains(t, err, "service: data since")

	_, err = service.NewSite(nil, nil, nil, nil)
	require.Error(t, err)
}
