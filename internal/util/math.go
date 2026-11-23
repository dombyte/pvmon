package util

import "math"

// Round rounds v to the given number of decimals (half away from zero).
func Round(v float64, decimals int) float64 {
	p := math.Pow10(decimals)
	return math.Round(v*p) / p
}

// Clamp limits v to [lo, hi].
func Clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

// AlmostEqual reports whether a and b differ by at most eps.
func AlmostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}
