package ordering

import "testing"

// TestCalls calls Double without checking the result. It runs first in declaration order but detects no mutant.
func TestCalls(t *testing.T) {
	_ = Double(3)
}

func TestDouble(t *testing.T) {
	if got := Double(3); got != 6 {
		t.Errorf("Double(3) = %d, want 6", got)
	}
}
