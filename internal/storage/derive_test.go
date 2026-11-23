package storage

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSumEnergyMany(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStorage(t)
	r := s.Read()
	empty, err := r.SumEnergyMany(ctx, nil, 0, 10, 10)
	require.NoError(t, err)
	assert.Empty(t, empty)

	require.NoError(t, s.Collector().Flush(ctx, Batch{Energy: []EnergyBucket{
		{SeriesID: 1, Slot: 10, Wh: 1},
		{SeriesID: 1, Slot: 11, Wh: 2, Flags: FlagGap},
		{SeriesID: 1, Slot: 12, Wh: 4, Flags: FlagEstimated},
		{SeriesID: 1, Slot: 13, Wh: 8},
		{SeriesID: 1, Slot: 20, Wh: 100},
		{SeriesID: 2, Slot: 10, Wh: 5},
		{SeriesID: 2, Slot: 11, Wh: 6},
	}}))
	got, err := r.SumEnergyMany(ctx, []int64{1, 2, 3}, 10, 20, 13)
	require.NoError(t, err)
	require.Len(t, got, 2, "absent series 3 is missing")
	assert.Equal(t, EnergySum{Wh: 15, Buckets: 4, Closed: 2, Estimated: true}, got[1])
	assert.Equal(t, EnergySum{Wh: 11, Buckets: 2, Closed: 2}, got[2])

	got, err = r.SumEnergyMany(ctx, []int64{2}, 11, 12, 100)
	require.NoError(t, err)
	assert.InDelta(t, 6, got[2].Wh, 0)

	many := make([]int64, 0, 1200)
	for i := int64(1); i <= 1200; i++ {
		many = append(many, i)
	}
	got, err = r.SumEnergyMany(ctx, many, 10, 20, 13)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestCarry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	r := s.Read()
	got, err := r.Carry(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	_, err = db.ExecContext(ctx, `INSERT INTO carry(key, wh, before_slot) VALUES
		('1', 1.5, 0), ('2', 2.5, 0), ('site/consumption', 9, 0), ('x1', 3, 0)`)
	require.NoError(t, err)
	got, err = r.Carry(ctx, []int64{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, map[int64]float64{1: 1.5, 2: 2.5}, got)
	// non-integer keys in the table are skipped while scanning
	rows, err := db.QueryContext(ctx, `SELECT key, wh FROM carry`)
	require.NoError(t, err)
	out := map[int64]float64{}
	require.NoError(t, scanCarry(rows, out))
	assert.Len(t, out, 2)
}

func TestDerivedCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStorage(t)
	d := s.Derive()
	require.NoError(t, d.CachePut(ctx, 1, 0, nil))
	got, err := d.CacheGet(ctx, nil, 1)
	require.NoError(t, err)
	assert.Empty(t, got)

	require.NoError(t, d.CachePut(ctx, 1, 0, map[string]string{"a|day|x": `{"v":1}`, "b": "2"}))
	got, err = d.CacheGet(ctx, []string{"a|day|x", "b", "c"}, 1)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a|day|x": `{"v":1}`, "b": "2"}, got)

	got, err = d.CacheGet(ctx, []string{"a|day|x"}, 2)
	require.NoError(t, err)
	assert.Empty(t, got, "other config_rev")

	require.NoError(t, d.CachePut(ctx, 2, 0, map[string]string{"b": "3"}))
	got, err = d.CacheGet(ctx, []string{"b"}, 1)
	require.NoError(t, err)
	assert.Empty(t, got)
	got, err = d.CacheGet(ctx, []string{"b"}, 2)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"b": "3"}, got)

	entries := map[string]string{}
	keys := make([]string, 0, 1300)
	for i := range 1300 {
		k := fmt.Sprintf("k%d", i)
		entries[k] = k
		keys = append(keys, k)
	}
	require.NoError(t, d.CachePut(ctx, 5, 0, entries))
	got, err = d.CacheGet(ctx, keys, 5)
	require.NoError(t, err)
	assert.Equal(t, entries, got)
}

func TestDeriveErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	require.NoError(t, db.Close())
	r, d := s.Read(), s.Derive()
	_, err := r.SumEnergyMany(ctx, []int64{1}, 0, 1, 1)
	require.ErrorContains(t, err, "storage: sum energy many")
	_, err = r.Carry(ctx, []int64{1})
	require.ErrorContains(t, err, "storage: carry")
	_, err = d.CacheGet(ctx, []string{"a"}, 1)
	require.ErrorContains(t, err, "storage: cache get")
	require.Error(t, d.CachePut(ctx, 1, 0, map[string]string{"a": "b"}))
}

func TestDeriveCachePutRollsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	_, err := db.ExecContext(ctx, `CREATE TRIGGER nocache BEFORE INSERT ON derived_cache
		WHEN new.key = 'bad' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	require.NoError(t, err)
	err = s.Derive().CachePut(ctx, 1, 0, map[string]string{"a": "1", "bad": "2"})
	require.ErrorContains(t, err, "cache put bad")
	got, err := s.Derive().CacheGet(ctx, []string{"a"}, 1)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestCachePutAfterPastWrite: a value computed before a write to a past slot is not stored
// after it, or the cache would keep the stale value for good.
func TestCachePutAfterPastWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStorage(t)
	d := s.Derive()
	key := []string{"1|day|1970-01-01"}
	entries := map[string]string{key[0]: `{"v":1}`}

	gen := d.CacheGen()
	require.NoError(t, s.Collector().Flush(ctx, Batch{
		TodayStart: 100, Energy: []EnergyBucket{{SeriesID: 1, Slot: 10, Wh: 1}},
	}))
	require.NoError(t, d.CachePut(ctx, 1, gen, entries))
	got, err := d.CacheGet(ctx, key, 1)
	require.NoError(t, err)
	assert.Empty(t, got, "computed before the past write: not stored")

	gen = d.CacheGen()
	require.NoError(t, s.Collector().Flush(ctx, Batch{
		TodayStart: 100, Energy: []EnergyBucket{{SeriesID: 1, Slot: 200, Wh: 1}},
	}))
	require.NoError(t, d.CachePut(ctx, 1, gen, entries))
	got, err = d.CacheGet(ctx, key, 1)
	require.NoError(t, err)
	assert.Len(t, got, 1, "a write to today keeps the generation")
}
