package c

// Double calls a cgo function. This file has no import "C", so it is mutated.
func Double(n int) int { return Add(n, n) }

// Inc adds 1 to n.
func Inc(n int) int { return n + 1 }
