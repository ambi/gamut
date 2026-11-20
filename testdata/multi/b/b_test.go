package b

import "testing"

func TestScale(t *testing.T) {
	if got := Scale(3, 2); got != 6 {
		t.Errorf("Scale(3, 2) = %d, want 6", got)
	}
}

func TestNeg(t *testing.T) {
	if got := Neg(2); got > 0 {
		t.Errorf("Neg(2) = %d, want negative", got)
	}
}
