package reuse

import "testing"

func TestClamp(t *testing.T) {
	cases := []struct{ v, want int }{{-1, 0}, {11, 10}, {5, 5}}
	for _, c := range cases {
		if got := Clamp(c.v, 0, 10); got != c.want {
			t.Errorf("Clamp(%d, 0, 10) = %d, want %d", c.v, got, c.want)
		}
	}
}

// TestScore は Score(0) で v >= 0 の境界を通るが、> に変えても結果は 0 のままなので、その変異体は生き残る。
func TestScore(t *testing.T) {
	cases := []struct{ v, want int }{{0, 0}, {60, 100}, {-5, 0}}
	for _, c := range cases {
		if got := Score(c.v); got != c.want {
			t.Errorf("Score(%d) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestTwice(t *testing.T) {
	if got := Twice(3); got != 6 {
		t.Errorf("Twice(3) = %d, want 6", got)
	}
}
