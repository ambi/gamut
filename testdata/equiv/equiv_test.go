package equiv

import "testing"

func TestWeak(t *testing.T) {
	if Scale(3) < 0 || Name(1) < -5 {
		t.Fatal("wrong")
	}
}
