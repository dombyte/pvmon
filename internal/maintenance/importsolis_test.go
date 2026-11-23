package maintenance_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/database"
	"github.com/dombyte/pvmon/internal/maintenance"
	"github.com/dombyte/pvmon/internal/maintenance/mocks"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	smocks "github.com/dombyte/pvmon/internal/storage/mocks"
	"github.com/dombyte/pvmon/internal/util/clocktest"
	"github.com/dombyte/pvmon/profiles"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	return loc
}

// solisFixture creates the solis test database from testdata/solis.sql.
func solisFixture(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "solis.db")
	db, err := database.Open(ctx, path)
	require.NoError(t, err)
	script, err := os.ReadFile(filepath.Join("testdata", "solis.sql"))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(script))
	require.NoError(t, err)
	require.NoError(t, db.Close())
	ro, err := database.OpenReadOnly(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ro.Close() })
	return ro
}

func solisSource(t *testing.T) *maintenance.SolisDB {
	t.Helper()
	src, err := maintenance.NewSolisDB(context.Background(), solisFixture(t))
	require.NoError(t, err)
	return src
}

func loadProfile(t *testing.T, id string) *profile.Loaded {
	t.Helper()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	p, err := l.Load(id, ".")
	require.NoError(t, err)
	return p
}

func solisSite(t *testing.T) *site.Site {
	t.Helper()
	s, err := site.Build(site.Spec{
		Name: "Home",
		Connections: map[string]site.ConnectionSpec{
			"lan": {Address: "tcp://10.0.0.20", Timeout: time.Second},
		},
		Devices: map[string]site.DeviceSpec{
			"wr1": {
				Connection: "lan", Unit: 1, Profile: loadProfile(t, "solis/s6-hybrid"),
				Interval: 5 * time.Second, Rollover: "23:59",
			},
		},
		Meters: map[string]site.MeterSpec{
			"pv":      {Device: "wr1", Source: "pv"},
			"battery": {Device: "wr1", Source: "battery"},
			"grid":    {Device: "wr1", Source: "grid"},
		},
	})
	require.NoError(t, err)
	return s
}

const mapYAML = `
energy:
  pv_energy: pv/energy_out
  battery_charge: battery/energy_in
status:
  device: wr1
  registers:
    solis_status: operating
`

func testJob(t *testing.T, target storage.ImportStore) maintenance.ImportJob {
	t.Helper()
	m, err := maintenance.ParseSolisMap([]byte(mapYAML))
	require.NoError(t, err)
	series, err := m.Resolve(solisSite(t))
	require.NoError(t, err)
	loc := berlin(t)
	return maintenance.ImportJob{
		Source: solisSource(t), Target: target, Map: m, Series: series, Location: loc,
		Clock: clocktest.New(time.Date(2026, 4, 1, 12, 0, 0, 0, loc)), Name: "solis:solis.db",
	}
}

// lastSlot is the last 15-minute slot before the given UTC instant (the next midnight).
func lastSlot(next time.Time) int64 { return next.Unix()/storage.SlotSeconds - 1 }

func TestImportSolis(t *testing.T) {
	t.Parallel()
	target := smocks.NewMockImportStore(t)
	target.EXPECT().ImportSource(mock.Anything).Return("", nil)
	target.EXPECT().FirstSlot(mock.Anything).Return(0, false, nil)
	var got storage.ImportBatch
	target.EXPECT().Import(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, b storage.ImportBatch) error {
			got = b
			return nil
		})
	rep, err := maintenance.ImportSolis(context.Background(), testJob(t, target))
	require.NoError(t, err)

	assert.Equal(t, "2026-04-01", rep.Cutoff)
	assert.Equal(t, []string{"household_energy"}, rep.Unmapped)
	assert.Equal(t, []maintenance.SeriesReport{
		{
			Key: "battery_charge", Target: "battery/energy_in", Days: 1, KWh: 4, Negative: 1,
			TotalBelow: true,
		},
		{Key: "pv_energy", Target: "pv/energy_out", Days: 4, KWh: 30.75, Later: 1, CarryKWh: 10},
	}, rep.Series)
	assert.Equal(t, 2, rep.Status)
	assert.Equal(t, []string{"mystery_fault"}, rep.StatusKeys)
	assert.Equal(t, []maintenance.PeriodMismatch{
		{Key: "pv_energy", Level: "monthly", Period: "2026-02", Stored: 150},
		{
			Key: "pv_energy", Level: "monthly", Period: "2026-03", Stored: 40.75, Daily: 33.75 - 3,
			Days: 4,
		},
	}, rep.Mismatches, "a zero month without days is no data; April and 2026 match")

	assert.Equal(t, "solis:solis.db", got.Source)
	require.Len(t, got.Series, 2)
	flags := storage.FlagEstimated | storage.FlagImported
	pv := got.Series[1]
	assert.Equal(t, storage.SeriesKey{Meter: "pv", Channel: "energy_out"}, pv.Def.SeriesKey)
	assert.Equal(t, []storage.EnergyBucket{
		{Slot: lastSlot(time.Date(2026, 3, 28, 23, 0, 0, 0, time.UTC)), Wh: 10500, Flags: flags},
		{Slot: lastSlot(time.Date(2026, 3, 29, 22, 0, 0, 0, time.UTC)), Wh: 12250, Flags: flags},
		{Slot: lastSlot(time.Date(2026, 3, 30, 22, 0, 0, 0, time.UTC)), Wh: 0, Flags: flags},
		{Slot: lastSlot(time.Date(2026, 3, 31, 22, 0, 0, 0, time.UTC)), Wh: 8000, Flags: flags},
	}, pv.Buckets, "last slot of each local day, across the DST change")
	assert.InDelta(t, 10000, pv.Carry, 1e-6, "total minus every solis daily value")
	assert.Zero(t, got.Series[0].Carry, "a total below the daily sum is never carried")
	assert.Equal(t, []storage.StatusEvent{
		{Device: "wr1", Register: "operating", At: time.Date(2026, 3, 30, 8, 0, 0, 0,
			time.UTC).UnixMilli(), Code: 4112},
		{Device: "wr1", Register: "operating", At: time.Date(2026, 3, 30, 8, 5, 0, 0,
			time.UTC).UnixMilli(), Code: 3},
	}, got.Status)
}

func TestImportSolisStopsAtFirstStoredDay(t *testing.T) {
	t.Parallel()
	target := smocks.NewMockImportStore(t)
	target.EXPECT().ImportSource(mock.Anything).Return("", nil)
	// pvmon has stored data since 2026-03-30 14:00 Berlin: that whole day stays its own.
	first := time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC).Unix() / storage.SlotSeconds
	target.EXPECT().FirstSlot(mock.Anything).Return(first, true, nil)
	var got storage.ImportBatch
	target.EXPECT().Import(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, b storage.ImportBatch) error {
			got = b
			return nil
		})
	rep, err := maintenance.ImportSolis(context.Background(), testJob(t, target))
	require.NoError(t, err)
	assert.Equal(t, "2026-03-30", rep.Cutoff)
	assert.Len(t, got.Series[1].Buckets, 2)
	assert.Equal(t, 3, rep.Series[1].Later)
	assert.InDelta(t, 10000, got.Series[1].Carry, 1e-6, "carry does not depend on the cutoff")
	assert.Empty(t, got.Status, "status from the first stored day on stays pvmon's")
}

func TestImportSolisRefusesSecondImport(t *testing.T) {
	t.Parallel()
	target := smocks.NewMockImportStore(t)
	target.EXPECT().ImportSource(mock.Anything).Return("solis:old.db", nil)
	_, err := maintenance.ImportSolis(context.Background(), testJob(t, target))
	require.ErrorIs(t, err, storage.ErrAlreadyImported)
	require.ErrorContains(t, err, "solis:old.db")
}

func TestImportSolisErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	cases := []struct {
		name  string
		setup func(target *smocks.MockImportStore, src *mocks.MockSolisSource)
	}{
		{"import source", func(tg *smocks.MockImportStore, _ *mocks.MockSolisSource) {
			tg.EXPECT().ImportSource(mock.Anything).Return("", boom)
		}},
		{"first slot", func(tg *smocks.MockImportStore, _ *mocks.MockSolisSource) {
			tg.EXPECT().ImportSource(mock.Anything).Return("", nil)
			tg.EXPECT().FirstSlot(mock.Anything).Return(0, false, boom)
		}},
		{"daily", func(tg *smocks.MockImportStore, src *mocks.MockSolisSource) {
			ready(tg)
			src.EXPECT().Values(mock.Anything, maintenance.LevelDaily).Return(nil, boom)
		}},
		{"total", func(tg *smocks.MockImportStore, src *mocks.MockSolisSource) {
			ready(tg)
			src.EXPECT().Values(mock.Anything, maintenance.LevelDaily).Return(nil, nil)
			src.EXPECT().Values(mock.Anything, maintenance.LevelTotal).Return(nil, boom)
		}},
		{"bad day", func(tg *smocks.MockImportStore, src *mocks.MockSolisSource) {
			ready(tg)
			src.EXPECT().Values(mock.Anything, maintenance.LevelDaily).Return(
				[]maintenance.SolisValue{{Base: "pv_energy", Period: "x", KWh: 1}}, nil)
			src.EXPECT().Values(mock.Anything, maintenance.LevelTotal).Return(nil, nil)
		}},
		{"status", func(tg *smocks.MockImportStore, src *mocks.MockSolisSource) {
			ready(tg)
			src.EXPECT().Values(mock.Anything, mock.Anything).Return(nil, nil)
			src.EXPECT().Status(mock.Anything).Return(nil, boom)
		}},
		{"mismatch daily", func(tg *smocks.MockImportStore, src *mocks.MockSolisSource) {
			ready(tg)
			src.EXPECT().Values(mock.Anything, maintenance.LevelDaily).Return(nil, nil).Once()
			src.EXPECT().Values(mock.Anything, maintenance.LevelTotal).Return(nil, nil)
			src.EXPECT().Status(mock.Anything).Return(nil, nil)
			src.EXPECT().Values(mock.Anything, maintenance.LevelDaily).Return(nil, boom)
		}},
		{"mismatch monthly", func(tg *smocks.MockImportStore, src *mocks.MockSolisSource) {
			ready(tg)
			src.EXPECT().Values(mock.Anything, maintenance.LevelDaily).Return(nil, nil)
			src.EXPECT().Values(mock.Anything, maintenance.LevelTotal).Return(nil, nil)
			src.EXPECT().Status(mock.Anything).Return(nil, nil)
			src.EXPECT().Values(mock.Anything, maintenance.LevelMonthly).Return(nil, boom)
		}},
		{"write", func(tg *smocks.MockImportStore, src *mocks.MockSolisSource) {
			ready(tg)
			src.EXPECT().Values(mock.Anything, mock.Anything).Return(nil, nil)
			src.EXPECT().Status(mock.Anything).Return(nil, nil)
			tg.EXPECT().Import(mock.Anything, mock.Anything).Return(boom)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target := smocks.NewMockImportStore(t)
			src := mocks.NewMockSolisSource(t)
			tc.setup(target, src)
			job := testJob(t, target)
			job.Source = src
			_, err := maintenance.ImportSolis(context.Background(), job)
			require.Error(t, err)
		})
	}
}

func ready(tg *smocks.MockImportStore) {
	tg.EXPECT().ImportSource(mock.Anything).Return("", nil)
	tg.EXPECT().FirstSlot(mock.Anything).Return(0, false, nil)
}
