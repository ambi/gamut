package unrelated

import "testing"

func TestMeters(t *testing.T) {
	if got := Meters("3"); got != "3m" {
		t.Errorf("Meters(3) = %q, want 3m", got)
	}
}

func TestMetersEmpty(t *testing.T) {
	if got := Meters(""); got != "m" {
		t.Errorf("Meters(\"\") = %q, want m", got)
	}
}
