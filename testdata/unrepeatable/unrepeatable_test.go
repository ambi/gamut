package unrepeatable

import "testing"

// TestSwitch checks without resetting on. Run a second time in the same process, on flips back to false and it fails.
func TestSwitch(t *testing.T) {
	Switch(1)
	if !on {
		t.Fatal("not switched on")
	}
}

func TestTwice(t *testing.T) {
	if Twice(3) != 6 {
		t.Fatal("wrong")
	}
}
