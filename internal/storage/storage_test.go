package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/database"
)

var t0 = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func newStorage(t *testing.T) (*Storage, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pvmon.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, database.Migrate(ctx, db, nil, t0))
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	s, err := New(db, berlin)
	require.NoError(t, err)
	return s, db
}

func ptr(f float64) *float64 { return &f }

func TestSeriesAndFlush(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStorage(t)
	c, r := s.Collector(), s.Read()
	defs := []SeriesDef{
		{SeriesKey: SeriesKey{"pv", "power"}, Kind: 1, Origin: 1, Unit: "W"},
		{SeriesKey: SeriesKey{"pv", "energy_out"}, Kind: 2, Origin: 1, Unit: "Wh", Hidden: true},
	}
	ids, err := c.EnsureSeries(ctx, 100, defs)
	require.NoError(t, err)
	again, err := c.EnsureSeries(ctx, 200, defs)
	require.NoError(t, err)
	assert.Equal(t, ids, again, "existing active series are reused")
	pw, en := ids[SeriesKey{"pv", "power"}], ids[SeriesKey{"pv", "energy_out"}]
	id, err := r.SeriesID(ctx, SeriesKey{"pv", "power"})
	require.NoError(t, err)
	assert.Equal(t, pw, id)
	_, err = r.SeriesID(ctx, SeriesKey{"x", "y"})
	require.ErrorIs(t, err, ErrNotFound)

	b := Batch{
		Gauges:   []GaugeBucket{{SeriesID: pw, Slot: 100, Avg: 100, Min: 50, Max: 150, Cover: 60}},
		Energy:   []EnergyBucket{{SeriesID: en, Slot: 100, Wh: 10, Counter: ptr(1000)}},
		Counters: []CounterState{{SeriesID: en, Raw: ptr(1000), At: 5, Remainder: 0.25}},
	}
	require.NoError(t, c.Flush(ctx, b))
	b = Batch{
		Gauges: []GaugeBucket{{SeriesID: pw, Slot: 100, Avg: 200, Min: 10, Max: 300, Cover: 900}},
		Energy: []EnergyBucket{
			{SeriesID: en, Slot: 100, Wh: 5, Flags: FlagGap},
			{SeriesID: en, Slot: 101, Wh: 1, Flags: FlagEstimated},
		},
		Counters: []CounterState{{SeriesID: en, Raw: ptr(1016), At: 6, Resolution: 0.1, ResetAt: 1, Seen: 7}},
	}
	require.NoError(t, c.Flush(ctx, b))

	g, err := r.GaugeBuckets(ctx, pw, 0, 1000)
	require.NoError(t, err)
	require.Len(t, g, 1)
	assert.InDelta(t, (100*60+200*900)/960.0, g[0].Avg, 1e-9)
	assert.InDelta(t, 10, g[0].Min, 0)
	assert.InDelta(t, 300, g[0].Max, 0)
	assert.Equal(t, 900, g[0].Cover, "capped at the slot length")

	e, err := r.EnergyBuckets(ctx, en, 0, 1000)
	require.NoError(t, err)
	require.Len(t, e, 2)
	assert.InDelta(t, 15, e[0].Wh, 0, "energy is added")
	assert.InDelta(t, 1000, *e[0].Counter, 0, "counter kept when the flush has none")
	assert.Equal(t, FlagGap, e[0].Flags)
	tot, err := r.SumEnergy(ctx, en, 0, 1000)
	require.NoError(t, err)
	assert.Equal(t, EnergyTotal{Wh: 16, Buckets: 2, Flags: FlagGap | FlagEstimated}, tot)
	tot, err = r.SumEnergy(ctx, en, 500, 1000)
	require.NoError(t, err)
	assert.Equal(t, EnergyTotal{}, tot)

	st, err := c.CounterStates(ctx, []int64{en, 999})
	require.NoError(t, err)
	require.Len(t, st, 1)
	assert.InDelta(t, 1016, *st[en].Raw, 0)
	assert.Equal(t, int64(1), st[en].ResetAt)
	assert.Equal(t, int64(7), st[en].Seen)
	empty, err := c.CounterStates(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestInvalidatePastSlots(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	keys := []string{
		"site/pv|day|2026-11-18", "site/pv|day|2026-11-17", "site/pv|month|2026-11",
		"site/pv|month|2026-10", "site/pv|year|2026", "site/pv|year|2025", "site/pv|total",
	}
	for _, k := range keys {
		_, err := db.ExecContext(ctx, `INSERT INTO derived_cache(key, config_rev, value)
			VALUES(?, 1, '{}')`, k)
		require.NoError(t, err)
	}
	ids, err := s.Collector().EnsureSeries(ctx, 0, []SeriesDef{{
		SeriesKey: SeriesKey{"pv", "e"},
		Kind:      2, Unit: "Wh",
	}})
	require.NoError(t, err)
	// 2026-11-18 23:30 Europe/Berlin = 22:30 UTC, written while today is 2026-11-19
	slot := time.Date(2026, 11, 18, 22, 30, 0, 0, time.UTC).Unix() / SlotSeconds
	today := time.Date(2026, 11, 18, 23, 0, 0, 0, time.UTC).Unix() / SlotSeconds
	b := Batch{TodayStart: today, Energy: []EnergyBucket{
		{SeriesID: ids[SeriesKey{"pv", "e"}], Slot: slot, Wh: 1},
		{SeriesID: ids[SeriesKey{"pv", "e"}], Slot: today, Wh: 1},
	}}
	require.NoError(t, s.Collector().Flush(ctx, b))
	rows, err := db.QueryContext(ctx, `SELECT key FROM derived_cache ORDER BY key`)
	require.NoError(t, err)
	defer rows.Close()
	var left []string
	for rows.Next() {
		var k string
		require.NoError(t, rows.Scan(&k))
		left = append(left, k)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{
		"site/pv|day|2026-11-17", "site/pv|month|2026-10",
		"site/pv|year|2025",
	}, left)
}

func TestWriteDomains(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStorage(t)
	noop := func(*sql.Tx) error { return nil }
	cases := []struct {
		d     Domain
		table string
		ok    bool
	}{
		{DomainCollector, "b15_energy", true},
		{DomainCollector, "derived_cache", false},
		{DomainCollector, "user", false},
		{DomainDerive, "carry", true},
		{DomainDerive, "b15_energy", false},
		{DomainConfig, "audit_log", true},
		{DomainConfig, "session", false},
		{DomainAuth, "session", true},
		{DomainAuth, "config_object", false},
		{DomainCLI, "b15_energy", true},
	}
	for _, tc := range cases {
		err := s.write(ctx, tc.d, []string{tc.table}, noop)
		if tc.ok {
			require.NoError(t, err, "%s %s", tc.d, tc.table)
			continue
		}
		var de *DomainError
		require.ErrorAs(t, err, &de, "%s %s", tc.d, tc.table)
		assert.Equal(t, tc.table, de.Table)
		assert.Contains(t, de.Error(), tc.d.String()+" may not write "+tc.table)
	}
}

func TestStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStorage(t)
	c := s.Collector()
	require.NoError(t, c.AddStatus(ctx, []StatusEvent{
		{Device: "wr1", Register: "operating", At: 1, Code: 3},
		{Device: "wr1", Register: "operating", At: 2, Code: 0x1015},
		{Device: "wr1", Register: "faults", At: 1, Code: 0},
		{Device: "meter", Register: "errors", At: 1, Code: 1},
	}))
	last, err := c.LastStatus(ctx, "wr1")
	require.NoError(t, err)
	assert.Len(t, last, 2)
	assert.Equal(t, int64(0x1015), last["operating"].Code)

	ev, err := s.Read().StatusEvents(ctx, "wr1", 2, 10, 10)
	require.NoError(t, err)
	assert.Equal(t, []StatusEvent{
		{Device: "wr1", Register: "operating", At: 2, Code: 0x1015},
		{Device: "wr1", Register: "faults", At: 1, Code: 0},
		{Device: "wr1", Register: "operating", At: 1, Code: 3},
	}, ev, "changes in range, then the state active at its start")
	ev, err = s.Read().StatusEvents(ctx, "wr1", 0, 10, 1)
	require.NoError(t, err)
	assert.Len(t, ev, 1, "limit")
}

func TestErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	_, err := New(nil, time.UTC)
	require.Error(t, err)
	require.NoError(t, db.Close())
	c, r := s.Collector(), s.Read()
	_, err = c.EnsureSeries(ctx, 0, []SeriesDef{{}})
	require.Error(t, err)
	_, err = r.StatusEvents(ctx, "wr1", 0, 1, 1)
	require.Error(t, err)
	_, err = c.CounterStates(ctx, []int64{1})
	require.Error(t, err)
	require.Error(t, c.Flush(ctx, Batch{}))
	require.Error(t, c.AddStatus(ctx, nil))
	_, err = c.LastStatus(ctx, "x")
	require.Error(t, err)
	_, err = r.SumEnergy(ctx, 1, 0, 1)
	require.Error(t, err)
	_, err = r.EnergyBuckets(ctx, 1, 0, 1)
	require.Error(t, err)
	_, err = r.GaugeBuckets(ctx, 1, 0, 1)
	require.Error(t, err)
	_, err = r.SeriesID(ctx, SeriesKey{})
	require.Error(t, err)
}

func TestFailingStatementsRollBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStorage(t)
	c := s.Collector()
	// triggers make each statement fail inside its transaction
	_, err := s.db.ExecContext(ctx, `CREATE TRIGGER nogauge BEFORE INSERT ON b15_gauge
		BEGIN SELECT RAISE(ABORT, 'no'); END;
		CREATE TRIGGER noenergy BEFORE INSERT ON b15_energy BEGIN SELECT RAISE(ABORT, 'no'); END;
		CREATE TRIGGER nocounter BEFORE INSERT ON counter_state BEGIN SELECT RAISE(ABORT, 'no'); END;
		CREATE TRIGGER nostatus BEFORE INSERT ON status_event BEGIN SELECT RAISE(ABORT, 'no'); END;
		CREATE TRIGGER noseries BEFORE INSERT ON series BEGIN SELECT RAISE(ABORT, 'no'); END;`)
	require.NoError(t, err)
	require.ErrorContains(t, c.Flush(ctx, Batch{Gauges: []GaugeBucket{{SeriesID: 1}}}), "gauge")
	require.ErrorContains(t, c.Flush(ctx, Batch{Energy: []EnergyBucket{{SeriesID: 1}}}), "energy")
	require.ErrorContains(t, c.Flush(ctx, Batch{Counters: []CounterState{{SeriesID: 1}}}),
		"counter")
	require.ErrorContains(t, c.AddStatus(ctx, []StatusEvent{{Device: "d"}}), "status")
	_, err = c.EnsureSeries(ctx, 0, []SeriesDef{{SeriesKey: SeriesKey{"a", "b"}, Unit: "W"}})
	require.ErrorContains(t, err, "create series")
}

func TestFirstSlot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	_, ok, err := s.Read().FirstSlot(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "no data yet")

	ids, err := s.Collector().EnsureSeries(ctx, 100, []SeriesDef{
		{SeriesKey: SeriesKey{"pv", "power"}, Kind: 1, Origin: 1, Unit: "W"},
		{SeriesKey: SeriesKey{"pv", "energy_out"}, Kind: 2, Origin: 1, Unit: "Wh"},
	})
	require.NoError(t, err)
	pw, en := ids[SeriesKey{"pv", "power"}], ids[SeriesKey{"pv", "energy_out"}]
	require.NoError(t, s.Collector().Flush(ctx, Batch{
		Gauges: []GaugeBucket{{SeriesID: pw, Slot: 120, Avg: 1, Min: 1, Max: 1, Cover: 60}},
		Energy: []EnergyBucket{{SeriesID: en, Slot: 130, Wh: 1}},
	}))
	slot, ok, err := s.Read().FirstSlot(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int64(120), slot, "earliest over gauge and energy buckets")

	require.NoError(t, db.Close())
	_, _, err = s.Read().FirstSlot(ctx)
	require.Error(t, err)
}
