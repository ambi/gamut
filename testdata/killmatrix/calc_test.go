package calc

import "testing"

// TestPanicky reports failure by panicking. Even when the process dies, detections by later tests must be recorded.
func TestPanicky(t *testing.T) {
	if Add(2, 3) != 5 {
		panic("wrong sum")
	}
}

// TestNothing has no assertions.
func TestNothing(t *testing.T) {
	_ = Add(1, 2)
	_ = Sub(1, 2)
}

// TestAdd and TestAddAgain detect the same mutants, and TestBoth subsumes both.
func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("wrong sum")
	}
}

func TestAddAgain(t *testing.T) {
	if Add(4, 1) != 5 {
		t.Fatal("wrong sum")
	}
}

func TestBoth(t *testing.T) {
	if Add(2, 3) != 5 || Sub(5, 3) != 2 {
		t.Fatal("wrong")
	}
}
