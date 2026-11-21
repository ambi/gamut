// Package plain does not use cgo. It is judged even when the cgo package is skipped.
package plain

// Half divides n by 2.
func Half(n int) int { return n / 2 }
