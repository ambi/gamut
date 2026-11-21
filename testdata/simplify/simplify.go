// Package simplify has an unneeded branch and a call whose result nobody looks at. -reporter simplify lists them as candidates.
package simplify

var touched int

func touch() { touched++ }

// Abs returns the absolute value. The n == 0 branch returns the same as the last return, so it is not needed.
func Abs(n int) int {
	touch()
	if n < 0 {
		return -n
	}
	if n == 0 {
		return 0
	}
	return n
}

// Reset is not called by tests. Its statement removal is not a candidate but counts as not reached.
func Reset() { touch() }
