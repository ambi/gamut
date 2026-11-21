// Package slow has tests that take long per mutant. The end-to-end test that stops the process midway uses it.
package slow

// Score has many mutation sites.
func Score(a, b, c, d int) int {
	s := 0
	if a > b {
		s += a - b
	}
	if c < d {
		s += d - c
	}
	return s*2 + a*b - c*d
}
