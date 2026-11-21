package calc

import "testing"

func check(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

// TestAddSmall and TestAddBig both detect Add's two mutants (+ -> - and RETURN_ZERO).
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

// TestAddFive detects only RETURN_ZERO (5 - 0 is also 5).
func TestAddFive(t *testing.T) {
	if Add(5, 0) != 5 {
		t.Fatal("wrong")
	}
}

// TestAddZero reaches Add but detects no mutant.
func TestAddZero(t *testing.T) {
	if Add(0, 0) != 0 {
		t.Fatal("wrong")
	}
}

// TestMul uses the helper check.
func TestMul(t *testing.T) {
	check(t, Mul(2, 3), 6)
}
