package operators

import "testing"

func TestBitwise(t *testing.T) {
	if Flags(1, 2) != 3 || Mask(3, 3) != 3 || Double(2) != 4 {
		t.Fatal("wrong")
	}
}
