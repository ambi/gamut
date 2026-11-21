package flags

import "testing"

// TestHalf fails without -short. If the test flags were not passed, the baseline tests would break.
func TestHalf(t *testing.T) {
	if !testing.Short() {
		t.Fatal("this test needs -short")
	}
	if Half(8) != 4 {
		t.Fatal("Half")
	}
}
