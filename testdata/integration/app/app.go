// Package app calls lib.
package app

import "integration/lib"

// Sum3 returns the sum of three values.
func Sum3(a, b, c int) int {
	return lib.Add(lib.Add(a, b), c)
}

// Nine returns three times 3.
func Nine() int {
	return lib.Triple(3)
}
