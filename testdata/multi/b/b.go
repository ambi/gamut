// Package b imports the target package a.
package b

import "multi/a"

// Scale multiplies v by k, giving 0 when negative.
func Scale(v, k int) int {
	return a.Max(v*k, 0)
}

// Neg flips the sign of v.
func Neg(v int) int {
	return -v
}
