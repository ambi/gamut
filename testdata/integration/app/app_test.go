package app

import "testing"

func TestSum3(t *testing.T) {
	if got := Sum3(1, 2, 3); got != 6 {
		t.Errorf("Sum3(1, 2, 3) = %d, want 6", got)
	}
}

func TestNine(t *testing.T) {
	if got := Nine(); got != 9 {
		t.Errorf("Nine() = %d, want 9", got)
	}
}
