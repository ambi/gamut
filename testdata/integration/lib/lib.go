// Package lib has mutants that only the tests of calling packages can detect.
package lib

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a + b
}

// Triple returns three times v. lib's tests do not call it.
func Triple(v int) int {
	return v * 3
}

// Unit is a constant that unrelated only refers to.
const Unit = "m"
