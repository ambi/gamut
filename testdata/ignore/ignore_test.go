package ignore

import "testing"

func TestAll(t *testing.T) {
	if Sum(1, 2) != 3 || Cached(2, 3) != 6 || !Fast(1, 2) || !Ranged(1, 2) || NoReason(3, 1) != 2 {
		t.Fatal("wrong")
	}
}
