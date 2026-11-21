// Package a is a target package that b imports.
package a

// Max returns the larger of x and y.
func Max(x, y int) int {
	if x > y {
		return x
	}
	return y
}
