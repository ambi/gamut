package calc

import "testing"

func check(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

// TestAddSmall と TestAddBig は、Add の 2 体の変異体（+ -> - と RETURN_ZERO）をどちらも検出する。
func TestAddSmall(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("wrong")
	}
}

func TestAddBig(t *testing.T) {
	if Add(10, 20) != 30 {
		t.Fatal("wrong")
	}
}

// TestAddFive は RETURN_ZERO だけを検出する（5 - 0 も 5 である）。
func TestAddFive(t *testing.T) {
	if Add(5, 0) != 5 {
		t.Fatal("wrong")
	}
}

// TestAddZero は Add に到達するが、どの変異体も検出しない。
func TestAddZero(t *testing.T) {
	if Add(0, 0) != 0 {
		t.Fatal("wrong")
	}
}

// TestMul は補助の check を使う。
func TestMul(t *testing.T) {
	check(t, Mul(2, 3), 6)
}
