package maintenance_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/database"
	"github.com/dombyte/pvmon/internal/maintenance"
)

func TestSolisDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := solisSource(t)
	daily, err := src.Values(ctx, maintenance.LevelDaily)
	require.NoError(t, err)
	require.Len(t, daily, 8)
	assert.Equal(t, maintenance.SolisValue{
		Base: "battery_charge", Period: "2026-03-30",
		KWh: 4,
	}, daily[0], "DATE columns come back as plain dates")
	totals, err := src.Values(ctx, maintenance.LevelTotal)
	require.NoError(t, err)
	assert.Equal(t, []maintenance.SolisValue{
		{Base: "battery_charge", KWh: 2}, {Base: "pv_energy", KWh: 43.75},
	}, totals)
	years, err := src.Values(ctx, maintenance.LevelYearly)
	require.NoError(t, err)
	assert.Equal(t, []maintenance.SolisValue{{Base: "pv_energy", Period: "2026", KWh: 33.75}},
		years)
	_, err = src.Values(ctx, "weekly")
	require.ErrorContains(t, err, "unknown solis level")

	st, err := src.Status(ctx)
	require.NoError(t, err)
	require.Len(t, st, 4)
	assert.Equal(t, maintenance.SolisStatus{
		Key: "solis_status", At: time.Date(2026, 3, 30, 8, 0, 0, 0, time.UTC), Code: 4112,
	}, st[0])
}

func TestSolisDBErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "other.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = maintenance.NewSolisDB(ctx, db)
	require.ErrorIs(t, err, maintenance.ErrNotSolis)

	_, err = db.ExecContext(ctx, `CREATE TABLE daily_values (date TEXT, register_key TEXT,
		value TEXT); INSERT INTO daily_values VALUES ('2026-01-01', 'pv_energy_daily', 'x');
		CREATE TABLE error_data (timestamp TEXT, register_key TEXT, raw_value REAL);
		INSERT INTO error_data VALUES ('yesterday', 'solis_status', 1)`)
	require.NoError(t, err)
	src, err := maintenance.NewSolisDB(ctx, db)
	require.NoError(t, err)
	_, err = src.Values(ctx, maintenance.LevelDaily)
	require.ErrorContains(t, err, "scan solis daily")
	_, err = src.Values(ctx, maintenance.LevelMonthly)
	require.ErrorContains(t, err, "read solis monthly")
	_, err = src.Status(ctx)
	require.ErrorContains(t, err, "solis status solis_status")

	require.NoError(t, db.Close())
	_, err = maintenance.NewSolisDB(ctx, db)
	require.ErrorContains(t, err, "read solis schema")
	_, err = src.Status(ctx)
	require.ErrorContains(t, err, "read solis status")
}

func TestSolisMap(t *testing.T) {
	t.Parallel()
	s := solisSite(t)
	_, err := maintenance.ParseSolisMap([]byte("energy: { pv_energy: pv/energy_out }\nextra: 1"))
	require.ErrorIs(t, err, maintenance.ErrSolisMap)

	m, err := maintenance.ParseSolisMap([]byte(`
energy:
  pv_energy: pv/energy_out
  pv_again: pv/energy_out
  battery_charge: battery/power
  nothing: nowhere/energy_in
status:
  device: wr1
  registers: { solis_status: operating, grid_fault_1: no_such }
`))
	require.NoError(t, err)
	_, err = m.Resolve(s)
	require.ErrorIs(t, err, maintenance.ErrSolisMap)
	for _, want := range []string{
		`energy.battery_charge: "battery/power" is no energy series`,
		`energy.nothing: "nowhere/energy_in" is no energy series`,
		`energy.pv_energy: "pv/energy_out" is already the target of pv_again`,
		`status.registers.grid_fault_1: profile solis/s6-hybrid has no status "no_such"`,
	} {
		assert.ErrorContains(t, err, want)
	}

	m, err = maintenance.ParseSolisMap([]byte("status:\n  device: wr9\n  registers: { a: b }"))
	require.NoError(t, err)
	_, err = m.Resolve(s)
	require.ErrorContains(t, err, "energy: no series mapped")
	require.ErrorContains(t, err, `status.device: unknown device "wr9"`)
}
