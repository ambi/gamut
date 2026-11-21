package rerun

import "testing"

// TestIsAdult does not try the boundary 18, so the mutant turning >= into > survives.
func TestIsAdult(t *testing.T) {
	if !IsAdult(30) || IsAdult(5) {
		t.Error("IsAdult is wrong")
	}
}

func TestHalf(t *testing.T) {
	if got := Half(8); got != 4 {
		t.Errorf("Half(8) = %d, want 4", got)
	}
}
