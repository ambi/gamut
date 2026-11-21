package operators

import "testing"

func TestBitwise(t *testing.T) {
	if Flags(1, 2) != 3 || Mask(3, 3) != 3 || Double(2) != 4 {
		t.Fatal("wrong")
	}
}

func TestAssignments(t *testing.T) {
	if Total([]int{1, 2}) != 3 || Scaled(5, 1) != 5 || SetBit(2) != 3 || ClearHigh(0) != 0 {
		t.Fatal("wrong")
	}
}
