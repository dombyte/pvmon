package derive

import (
	"sort"

	"github.com/dombyte/pvmon/internal/storage"
)

// Agg is the energy of one series over a period with its quality (spec 5.4).
type Agg struct {
	Wh        float64  `json:"wh"`
	Data      bool     `json:"data"`              // at least one bucket exists
	Estimated bool     `json:"estimated"`         // any estimated or gap bucket
	Missing   []string `json:"missing,omitempty"` // incomplete local days, sorted
}

// Plus adds a later part of the same series (time sum): data if any part has data.
func (a Agg) Plus(b Agg) Agg {
	return Agg{
		Wh: a.Wh + b.Wh, Data: a.Data || b.Data, Estimated: a.Estimated || b.Estimated,
		Missing: union(a.Missing, b.Missing),
	}
}

// Combine adds coef × b as another term of a site quantity: data only if every term has
// data, incomplete where any term is.
func (a Agg) Combine(coef float64, b Agg) Agg {
	return Agg{
		Wh: a.Wh + coef*b.Wh, Data: a.Data && b.Data, Estimated: a.Estimated || b.Estimated,
		Missing: union(a.Missing, b.Missing),
	}
}

func union(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, d := range list {
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	sort.Strings(out)
	return out
}

// dayAgg turns the grouped sum of a day into an Agg: complete when every closed slot
// (expected) has a bucket without gap, or when the day was imported as one daily value.
func dayAgg(day string, sm storage.EnergySum, expected int) Agg {
	a := Agg{Wh: sm.Wh, Data: sm.Buckets > 0, Estimated: sm.Estimated}
	if sm.Closed < expected && !sm.Imported {
		a.Missing = []string{day}
	}
	return a
}
