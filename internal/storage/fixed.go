package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
)

// FixedScale is the fixed-point scale of stored bucket values: b15_gauge and b15_energy hold
// integers in 1/1000 of the channel unit (mWh, mW, 0.001 %, ...). An integer of up to
// three bytes replaces an eight-byte REAL, which makes buckets about 25 % smaller, and SQL
// sums are exact. 1/1000 is far below the resolution of any meter (spec 7.2).
const FixedScale = 1000

// maxFixed bounds a value so value × FixedScale stays exact in a float64 (2^53).
const maxFixed = 1 << 53 / FixedScale

// ErrNotStorable means a bucket value is NaN, infinite or too large to store.
var ErrNotStorable = errors.New("storage: value not storable")

// toFixed converts a value to its stored integer form.
func toFixed(v float64) (int64, error) {
	if math.IsNaN(v) || math.Abs(v) > maxFixed {
		return 0, fmt.Errorf("%w: %v", ErrNotStorable, v)
	}
	return int64(math.Round(v * FixedScale)), nil
}

// toFixedPtr converts an optional value; nil stays NULL.
func toFixedPtr(v *float64) (any, error) {
	if v == nil {
		return nil, nil
	}
	return toFixed(*v)
}

// fromFixed converts a stored integer back to the channel unit.
func fromFixed(v int64) float64 { return float64(v) / FixedScale }

// fromFixedNull converts an optional stored integer; NULL becomes nil.
func fromFixedNull(v sql.NullInt64) *float64 {
	if !v.Valid {
		return nil
	}
	f := fromFixed(v.Int64)
	return &f
}

// fixedAll converts several values, failing on the first one that is not storable.
func fixedAll(vs ...float64) ([]int64, error) {
	out := make([]int64, len(vs))
	for i, v := range vs {
		f, err := toFixed(v)
		if err != nil {
			return nil, err
		}
		out[i] = f
	}
	return out, nil
}
