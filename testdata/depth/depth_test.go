package depth

import "testing"

func TestTop(t *testing.T) {
	if Top(1) != 2 {
		t.Fatal("wrong")
	}
}

// TestLeaf は leaf を直接呼ぶ。leaf の変異箇所までの深さは 1 である。
func TestLeaf(t *testing.T) {
	t.Run("one", func(t *testing.T) {
		if leaf(1) != 2 {
			t.Fatal("wrong")
		}
	})
}
