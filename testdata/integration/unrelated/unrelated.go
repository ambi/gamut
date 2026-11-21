// Package unrelated imports lib but passes no mutation site.
package unrelated

import "integration/lib"

// Meters returns a string with a unit.
func Meters(s string) string {
	return s + lib.Unit
}
