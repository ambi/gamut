package plain

import "testing"

func TestHalf(t *testing.T) {
	if Half(8) != 4 {
		t.Fatal("wrong")
	}
}
