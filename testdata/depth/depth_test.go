package depth

import "testing"

func TestTop(t *testing.T) {
	if Top(1) != 2 {
		t.Fatal("wrong")
	}
}

// TestLeaf calls leaf directly. The depth to leaf's mutation site is 1.
func TestLeaf(t *testing.T) {
	t.Run("one", func(t *testing.T) {
		if leaf(1) != 2 {
			t.Fatal("wrong")
		}
	})
}
