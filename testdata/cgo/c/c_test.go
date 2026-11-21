package c

import "testing"

func TestDouble(t *testing.T) {
	if Double(3) != 6 {
		t.Fatal("wrong")
	}
}

func TestInc(t *testing.T) {
	if Inc(1) != 2 {
		t.Fatal("wrong")
	}
}
