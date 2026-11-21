package lib

import "testing"

// TestAdd calls Add without checking the result, so Add's mutants survive lib's tests.
func TestAdd(t *testing.T) {
	_ = Add(1, 2)
}
