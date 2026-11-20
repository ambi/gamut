package rerun

import "testing"

// TestIsAdult は境界の 18 を試さないので、>= を > にした変異体は生き残る。
func TestIsAdult(t *testing.T) {
	if !IsAdult(30) || IsAdult(5) {
		t.Error("IsAdult is wrong")
	}
}

func TestHalf(t *testing.T) {
	if got := Half(8); got != 4 {
		t.Errorf("Half(8) = %d, want 4", got)
	}
}
