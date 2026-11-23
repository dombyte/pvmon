package derive

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dombyte/pvmon/internal/storage"
)

func TestAggPlus(t *testing.T) {
	t.Parallel()
	a := Agg{Wh: 1, Data: true, Missing: []string{"2026-01-03"}}
	b := Agg{Wh: 2, Estimated: true, Missing: []string{"2026-01-01", "2026-01-03"}}
	got := a.Plus(b)
	assert.Equal(t, Agg{
		Wh: 3, Data: true, Estimated: true, Missing: []string{"2026-01-01", "2026-01-03"},
	}, got, "data if any part has data")
	assert.Equal(t, a, a.Plus(Agg{}))
	assert.Equal(t, b, Agg{}.Plus(b))
	assert.False(t, Agg{}.Plus(Agg{}).Data)
}

func TestAggCombine(t *testing.T) {
	t.Parallel()
	a := Agg{Wh: 10, Data: true, Missing: []string{"b"}}
	b := Agg{Wh: 4, Data: true, Estimated: true, Missing: []string{"a"}}
	got := a.Combine(-1, b)
	assert.Equal(t, Agg{
		Wh: 6, Data: true, Estimated: true, Missing: []string{"a", "b"},
	}, got)
	assert.False(t, a.Combine(1, Agg{}).Data, "data only if every term has data")
	assert.False(t, Agg{}.Combine(1, a).Data)
}

func TestDayAgg(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		sum      storage.EnergySum
		expected int
		want     Agg
	}{
		{
			"complete",
			storage.EnergySum{Wh: 5, Buckets: 96, Closed: 96},
			96,
			Agg{Wh: 5, Data: true},
		},
		{
			"gap",
			storage.EnergySum{Wh: 5, Buckets: 96, Closed: 95, Estimated: true},
			96,
			Agg{Wh: 5, Data: true, Estimated: true, Missing: []string{"d"}},
		},
		{
			"imported day",
			storage.EnergySum{Wh: 5, Buckets: 1, Closed: 1, Estimated: true, Imported: true},
			96,
			Agg{Wh: 5, Data: true, Estimated: true},
		},
		{"nodata", storage.EnergySum{}, 96, Agg{Missing: []string{"d"}}},
		{"nodata before first slot", storage.EnergySum{}, 0, Agg{}},
		{
			"more closed than expected",
			storage.EnergySum{Wh: 1, Buckets: 3, Closed: 3},
			2,
			Agg{Wh: 1, Data: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, dayAgg("d", tc.sum, tc.expected))
		})
	}
}
