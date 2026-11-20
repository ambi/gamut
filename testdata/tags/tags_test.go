package tags

import "testing"

func TestInc(t *testing.T) {
	if Inc(1) != 2 {
		t.Fatal("Inc")
	}
}
