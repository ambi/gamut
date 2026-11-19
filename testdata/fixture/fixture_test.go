package fixture

import "testing"

func TestLimit(t *testing.T) {
	if got := Limit(1, 5); got != 1 {
		t.Fatalf("Limit(1, 5) = %d", got)
	}
	if got := Limit(9, 5); got != 5 {
		t.Fatalf("Limit(9, 5) = %d", got)
	}
}

func TestSum(t *testing.T) {
	if got := Sum([]int{1, 2, 3}); got != 6 {
		t.Fatalf("Sum = %d", got)
	}
}

func TestNegate(t *testing.T) {
	if got := Negate(2); got != -2 {
		t.Fatalf("Negate(2) = %d", got)
	}
}

func TestReady(t *testing.T) {
	if !Ready("", false) {
		t.Fatal("Ready with empty name")
	}
}

func TestLogged(t *testing.T) {
	if Logged(1) != 1 {
		t.Fatal("Logged")
	}
}

func TestLoop(t *testing.T) {
	if Loop(3) != 3 {
		t.Fatal("Loop")
	}
}
