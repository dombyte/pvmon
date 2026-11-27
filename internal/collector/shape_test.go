package collector

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShapeIntegratesOneDirection(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	s := NewShape(-1) // e.g. battery energy_in: negative power charges
	assert.Nil(t, s.Weights(t0), "nothing covered yet")
	s.Add(-1000, t0)
	s.Add(-1000, t0) // same instant: ignored
	s.Add(-1000, t0.Add(30*time.Minute))
	s.Add(500, t0.Add(45*time.Minute)) // discharging: trapezoid of the clipped part
	w := s.Weights(t0)
	require.NotNil(t, w)
	assert.InDelta(t, 250, w[Slot(t0)], 1e-9)
	assert.InDelta(t, 250, w[Slot(t0.Add(15*time.Minute))], 1e-9)
	assert.InDelta(t, 125, w[Slot(t0.Add(30*time.Minute))], 1e-9, "1 kW → 0 W over 15 min")
	assert.Nil(t, s.Weights(t0.Add(-time.Minute)), "the span before the shape is unknown")
	late := s.Weights(t0.Add(10 * time.Minute))
	assert.InDelta(t, 250.0/3, late[Slot(t0)], 1e-9, "5 of the 15 covered minutes")
	assert.InDelta(t, 250, late[Slot(t0.Add(15*time.Minute))], 1e-9)
	assert.InDelta(t, 250, s.Weights(t0)[Slot(t0)], 1e-9, "the shape itself is unchanged")
	s.Cut(t0.Add(45 * time.Minute))
	assert.Empty(t, s.Weights(t0.Add(45*time.Minute)))
	assert.Nil(t, s.Weights(t0), "a cut forgets the old span")
}

func TestAddShaped(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	to := t0.Add(40 * time.Minute) // the last slot is covered for 10 min
	c := 7.0
	e := Energy{Wh: 100, From: t0, To: to, Counter: &c}
	for name, tc := range map[string]struct {
		w    map[int64]float64
		want []float64
	}{
		"shaped":    {w: map[int64]float64{Slot(t0): 1, Slot(to): 3}, want: []float64{25, 0, 75}},
		"no weight": {w: map[int64]float64{Slot(t0) - 1: 5}, want: []float64{37.5, 37.5, 25}},
		"nil":       {want: []float64{37.5, 37.5, 25}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a := NewAccumulator()
			a.AddShaped(1, e, tc.w)
			b := a.Batch(0)
			require.Len(t, b.Energy, len(tc.want), "every slot of the span gets a row")
			for i, want := range tc.want {
				assert.InDelta(t, want, b.Energy[i].Wh, 1e-9, i)
			}
			assert.Nil(t, b.Energy[0].Counter)
			assert.Equal(t, &c, b.Energy[2].Counter, "the counter ends the last slot")
		})
	}
}
