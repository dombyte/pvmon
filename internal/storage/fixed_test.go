package storage

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixedRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	c, r := s.Collector(), s.Read()
	b := Batch{
		Gauges: []GaugeBucket{{
			SeriesID: 1, Slot: 5, Avg: 738.09523809, Min: -0.0004, Max: 1e9,
			Cover: 300,
		}},
		Energy: []EnergyBucket{{
			SeriesID: 2, Slot: 5, Wh: 5.22768973477279,
			Counter: ptr(27400.000000000004),
		}},
	}
	require.NoError(t, c.Flush(ctx, b))
	// a second flush merges the average by covered seconds and rounds it to 1/1000
	b.Gauges[0].Avg, b.Energy[0].Wh, b.Energy[0].Counter = 0, 0.0004, nil
	require.NoError(t, c.Flush(ctx, b))

	g, err := r.GaugeBuckets(ctx, 1, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []GaugeBucket{{
		SeriesID: 1, Slot: 5, Avg: 369.048, Min: 0, Max: 1e9,
		Cover: 600,
	}}, g)
	e, err := r.EnergyBuckets(ctx, 2, 0, 10)
	require.NoError(t, err)
	require.Len(t, e, 1)
	assert.InDelta(t, 5.228, e[0].Wh, 0, "the sub-milli rest of each flush is rounded off")
	assert.InDelta(t, 27400, *e[0].Counter, 0)

	var typ string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT typeof(wh) || typeof(counter) FROM b15_energy`).Scan(&typ))
	assert.Equal(t, "integerinteger", typ)
}

func TestFixedRejectsUnstorable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStorage(t)
	c := s.Collector()
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 1e13} {
		require.ErrorIs(t, c.Flush(ctx, Batch{Energy: []EnergyBucket{{SeriesID: 1, Wh: v}}}),
			ErrNotStorable, v)
		require.ErrorIs(t, c.Flush(ctx, Batch{Energy: []EnergyBucket{{
			SeriesID: 1,
			Counter:  &v,
		}}}), ErrNotStorable, v)
		require.ErrorIs(t, c.Flush(ctx, Batch{Gauges: []GaugeBucket{{SeriesID: 1, Max: v}}}),
			ErrNotStorable, v)
	}
}
