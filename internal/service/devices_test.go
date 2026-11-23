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

	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/service/mocks"
	"github.com/dombyte/pvmon/internal/storage"
)

func newDevices(t *testing.T) (*service.Devices, *mocks.MockStatusReader) {
	t.Helper()
	snap := mocks.NewMockSnapshotSource(t)
	snap.EXPECT().Snapshot().Return(health.Snapshot{
		Devices: map[string]string{"wr1": "online"},
		Polls: map[string]health.Poll{
			"wr1": {At: time.UnixMilli(1000), Took: 312 * time.Millisecond, OK: true},
		},
	}).Maybe()
	st := mocks.NewMockStatusReader(t)
	svc, err := service.NewDevices(devSite(t), snap, st, time.UTC)
	require.NoError(t, err)
	return svc, st
}

func TestDevicesListAndGet(t *testing.T) {
	t.Parallel()
	svc, _ := newDevices(t)
	list := svc.List()
	require.Len(t, list, 3)
	assert.Equal(t, "meter", list[0].ID)
	assert.Equal(t, health.DeviceUnconfigured, list[0].State)
	wr1 := list[2]
	assert.Equal(t, "online", wr1.State)
	assert.Equal(t, []string{"backup", "battery", "pv"}, wr1.Meters)
	assert.InDelta(t, 5, wr1.IntervalS, 0)
	require.NotNil(t, wr1.LastPoll)
	assert.Equal(t, int64(312), wr1.LastPoll.TookMS)
	assert.True(t, wr1.LastPoll.OK)
	assert.Nil(t, list[0].LastPoll, "not polled yet")
	b, err := json.Marshal(list)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "s3cret", "credentials never leave the server")
	assert.NotContains(t, string(b), "10.0.0", "nor addresses")

	d, err := svc.Get("meter")
	require.NoError(t, err)
	assert.Equal(t, "meter", d.Netting)
	require.Len(t, d.Blocks, 1)
	assert.Equal(t, "grid", d.Blocks[0].Meter)
	assert.Contains(t, d.Blocks[0].Channels, service.DeviceChannel{Name: "power", Key: "grid/power", Unit: "W"})
	w, err := svc.Get("wr1")
	require.NoError(t, err)
	assert.Contains(t, w.Status, service.StatusRegister{
		Name: "operating", Label: profile.Text{"en": "Operating state", "de": "Betriebszustand"},
	})
	assert.Empty(t, w.Netting, "no grid meter on wr1")
	_, err = svc.Get("nope")
	require.ErrorIs(t, err, service.ErrNotFound)
}

func TestDevicesStatus(t *testing.T) {
	t.Parallel()
	svc, st := newDevices(t)
	ctx := context.Background()
	from, to := time.UnixMilli(1000), time.UnixMilli(5000)
	st.EXPECT().StatusEvents(mock.Anything, "wr1", int64(1000), int64(5000), 1000).Return(
		[]storage.StatusEvent{
			{Device: "wr1", Register: "operating", At: 2000, Code: 0},
			{Device: "wr1", Register: "operating", At: 900, Code: 3},
			{Device: "wr1", Register: "gone", At: 800, Code: 1},
			{Device: "wr1", Register: storage.ConnectionRegister, At: 700, Code: 1},
		}, nil).Once()
	ev, err := svc.Status(ctx, "wr1", from, to)
	require.NoError(t, err)
	require.Len(t, ev, 4)
	assert.Equal(t, "notice", ev[0].Level)
	assert.Equal(t, "Wartend", ev[0].Texts[0]["de"])
	assert.Equal(t, "ok", ev[1].Level)
	assert.Equal(t, "notice", ev[2].Level, "a removed register is a notice")
	assert.Empty(t, ev[2].Texts, "removed register: no text")
	assert.Equal(t, "offline", ev[3].Level)

	_, err = svc.Status(ctx, "nope", from, to)
	require.ErrorIs(t, err, service.ErrNotFound)
	_, err = svc.Status(ctx, "wr1", to, from)
	require.ErrorIs(t, err, service.ErrInvalidRange)
	st.EXPECT().StatusEvents(mock.Anything, "wr1", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("boom")).Once()
	_, err = svc.Status(ctx, "wr1", from, to)
	require.Error(t, err)
	_, err = service.NewDevices(nil, nil, nil, nil)
	require.Error(t, err)
}

func TestDevicesOverview(t *testing.T) {
	t.Parallel()
	svc, st := newDevices(t)
	ctx := context.Background()
	from, to := time.UnixMilli(1000), time.UnixMilli(9000)
	conn := storage.ConnectionRegister
	st.EXPECT().StatusEvents(mock.Anything, mock.Anything, int64(1000), int64(9000), 5000).
		RunAndReturn(func(_ context.Context, id string, _, _ int64, _ int,
		) ([]storage.StatusEvent, error) {
			if id != "wr1" {
				return nil, nil
			}
			return []storage.StatusEvent{ // newest first, then the state at from
				{Register: conn, At: 8000, Code: storage.ConnectionStopped},
				{Register: "operating", At: 6000, Code: 3},
				{Register: conn, At: 5000, Code: storage.ConnectionOnline},
				{Register: conn, At: 4000, Code: storage.ConnectionOffline},
				{Register: "operating", At: 3000, Code: 0x1015},
				{Register: conn, At: 3000, Code: storage.ConnectionOnline},
				{Register: "operating", At: 500, Code: 3},
			}, nil
		})
	devs, err := svc.Overview(ctx, from, to)
	require.NoError(t, err)
	var wr1 service.DeviceStatus
	for _, d := range devs {
		if d.ID == "wr1" {
			wr1 = d
		} else {
			assert.Empty(t, d.Changes, "no events: no changes")
		}
	}
	levels := make([]string, len(wr1.Changes))
	for i, c := range wr1.Changes {
		levels[i] = c.Level
	}
	assert.Equal(t, []string{"ok", "fault", "offline", "fault", "ok", "none"}, levels,
		"one poll decides once; offline wins over the registers")
	assert.Equal(t, int64(500), wr1.Changes[0].At.UnixMilli())

	_, err = svc.Overview(ctx, to, from)
	require.ErrorIs(t, err, service.ErrInvalidRange)
}
