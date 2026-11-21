// Package rerun is a fixed module for checking that, after adding a test for a survivor, only that mutant is rerun.
package rerun

// IsAdult reports whether age is 18 or more.
func IsAdult(age int) bool {
	return age >= 18
}

// Half returns half of v.
func Half(v int) int {
	return v / 2
}
