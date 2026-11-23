package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	im, r := s.Import(), s.Read()
	src, err := im.ImportSource(ctx)
	require.NoError(t, err)
	assert.Empty(t, src)

	// pvmon already runs: the series exists from slot 500 with a cached day value.
	pv := SeriesDef{SeriesKey: SeriesKey{"pv", "energy_out"}, Kind: 2, Origin: 1, Unit: "Wh"}
	ids, err := s.Collector().EnsureSeries(ctx, 500, []SeriesDef{pv})
	require.NoError(t, err)
	require.NoError(t, s.Derive().CachePut(ctx, 0, 0, map[string]string{"pv|day|x": "{}"}))

	flags := FlagEstimated | FlagImported
	bat := SeriesDef{SeriesKey: SeriesKey{"battery", "energy_in"}, Kind: 2, Origin: 1, Unit: "Wh"}
	require.NoError(t, im.Import(ctx, ImportBatch{
		Source: "solis:solis.db",
		Series: []ImportSeries{
			{Def: pv, Carry: 1500, Buckets: []EnergyBucket{
				{Slot: 300, Wh: 2000, Flags: flags}, {Slot: 200, Wh: 1000, Flags: flags},
			}},
			{Def: bat, Buckets: []EnergyBucket{{Slot: 250, Wh: 7, Flags: flags}}},
			{Def: SeriesDef{SeriesKey: SeriesKey{"backup", "energy_in"}}},
		},
		Status: []StatusEvent{
			{Device: "wr1", Register: "operating", At: 1000, Code: 3},
			{Device: "wr1", Register: "operating", At: 1000, Code: 3},
		},
	}))

	src, err = im.ImportSource(ctx)
	require.NoError(t, err)
	assert.Equal(t, "solis:solis.db", src)
	id := ids[pv.SeriesKey]
	sum, err := r.SumEnergyMany(ctx, []int64{id}, 0, 1000, 1000)
	require.NoError(t, err)
	assert.Equal(t, EnergySum{Wh: 3000, Buckets: 2, Closed: 2, Estimated: true, Imported: true},
		sum[id], "existing series reused")
	var from int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT active_from FROM series WHERE id=?`, id).
		Scan(&from))
	assert.Equal(t, int64(200), from, "active_from lowered to the first imported slot")
	carry, err := r.Carry(ctx, []int64{id})
	require.NoError(t, err)
	assert.Equal(t, map[int64]float64{id: 1500}, carry)
	batID, err := r.SeriesID(ctx, bat.SeriesKey)
	require.NoError(t, err)
	carry, err = r.Carry(ctx, []int64{batID})
	require.NoError(t, err)
	assert.Empty(t, carry, "no carry row for 0")
	_, err = r.SeriesID(ctx, SeriesKey{"backup", "energy_in"})
	require.ErrorIs(t, err, ErrNotFound, "a series without buckets is not created")
	got, err := s.Derive().CacheGet(ctx, []string{"pv|day|x"}, 0)
	require.NoError(t, err)
	assert.Empty(t, got, "derived cache dropped")
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM status_event`).Scan(&n))
	assert.Equal(t, 1, n)

	err = im.Import(ctx, ImportBatch{Source: "again"})
	require.ErrorIs(t, err, ErrAlreadyImported)
	require.ErrorContains(t, err, "solis:solis.db")
}

func TestImportErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pv := SeriesDef{SeriesKey: SeriesKey{"pv", "energy_out"}, Kind: 2, Origin: 1, Unit: "Wh"}
	one := func(b EnergyBucket) ImportBatch {
		return ImportBatch{Series: []ImportSeries{{Def: pv, Buckets: []EnergyBucket{b}}}}
	}
	nan := 0.0
	cases := []struct {
		name  string
		batch ImportBatch
		want  string
	}{
		{"occupied slot", one(EnergyBucket{Slot: 100, Wh: 1}), "import bucket"},
		{"bad wh", one(EnergyBucket{Slot: 1, Wh: nan / nan}), "not storable"},
		{"bad counter", one(EnergyBucket{Slot: 1, Counter: ptr(nan / nan)}), "import counter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, db := newStorage(t)
			ids, err := s.Collector().EnsureSeries(ctx, 100, []SeriesDef{pv})
			require.NoError(t, err)
			require.NoError(t, s.Collector().Flush(ctx, Batch{
				Energy: []EnergyBucket{{SeriesID: ids[pv.SeriesKey], Slot: 100, Wh: 5}},
			}))
			require.ErrorContains(t, s.Import().Import(ctx, tc.batch), tc.want)
			src, err := s.Import().ImportSource(ctx)
			require.NoError(t, err)
			assert.Empty(t, src, "rolled back")
			var wh int64
			require.NoError(t, db.QueryRowContext(ctx, `SELECT sum(wh) FROM b15_energy`).
				Scan(&wh))
			assert.Equal(t, int64(5000), wh)
		})
	}

	s, db := newStorage(t)
	require.NoError(t, db.Close())
	_, err := s.Import().ImportSource(ctx)
	require.ErrorContains(t, err, "import source")
	_, _, err = s.Import().FirstSlot(ctx)
	require.Error(t, err)
	require.Error(t, s.Import().Import(ctx, ImportBatch{}))
}
