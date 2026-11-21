// Package ignore has every form of disable comment. The end-to-end test checks that only the stopped mutants go into the report's ignored.
package ignore

// Sum has mutants that are not stopped.
func Sum(a, b int) int { return a + b }

// Cached is stopped by a comment at the end of a line.
func Cached(a, b int) int {
	return a * b //gamut:ignore -- the product is only a cache key
}

// Fast is stopped as a whole by a comment right before its declaration.
//
//gamut:ignore -- a fast path with the same result as Slow
func Fast(a, b int) bool {
	if a < b {
		return true
	}
	return a-b < 0
}

// Ranged stops only ARITHMETIC_BASE in a range, not the comparison.
func Ranged(a, b int) bool {
	//gamut:ignore-start ARITHMETIC_BASE -- defensive arithmetic
	x := a + b
	y := a - b
	//gamut:ignore-end
	return x > y
}

// NoReason has a comment without a reason. It is stopped, with a warning.
func NoReason(a, b int) int {
	return a - b //gamut:ignore
}
