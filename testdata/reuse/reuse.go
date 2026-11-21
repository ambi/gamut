// Package reuse is a fixed module for checking the reuse of verdicts.
// Score calls the helper Clamp, and Twice refers to no other declaration.
package reuse

// Clamp limits v to between lo and hi.
func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Score doubles a non-negative v capped at 100, and returns 0 for a negative v.
func Score(v int) int {
	if v >= 0 {
		return Clamp(v*2, 0, 100)
	}
	return 0
}

// Twice returns twice v.
func Twice(v int) int {
	return v + v
}
