// Package equiv has a mutant that compiles to the original's code and one that does not, both surviving.
package equiv

// The mutant turning x * 1 into x / 1 in Scale compiles to the original's code.
func Scale(x int) int { return x * 1 }

// The mutant turning x + 2 into x - 2 in Name compiles differently, but survives because the test is weak.
func Name(x int) int { return x + 2 }
