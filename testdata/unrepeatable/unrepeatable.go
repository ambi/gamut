// Package unrepeatable has a test that toggles a package variable. That test fails when run twice in the same process,
// and in a process with state left from the previous run, it may pass even with a mutant that removes the toggle.
package unrepeatable

var on bool

func toggle() { on = !on }

// Switch toggles on and returns times plus 1. The test does not check the return value, so the addition's mutants survive.
func Switch(times int) int {
	toggle()
	return times + 1
}

// Twice is checked by a test that can be repeated.
func Twice(n int) int { return n * 2 }
