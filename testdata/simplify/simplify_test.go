package simplify

import "testing"

func TestAbs(t *testing.T) {
	for _, c := range []struct{ in, want int }{{-2, 2}, {0, 0}, {3, 3}} {
		if got := Abs(c.in); got != c.want {
			t.Errorf("Abs(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
