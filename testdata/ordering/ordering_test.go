package ordering

import "testing"

// TestCalls は Double を呼ぶが結果を確かめない。宣言の順では先に走るが、変異体を検出しない。
func TestCalls(t *testing.T) {
	_ = Double(3)
}

func TestDouble(t *testing.T) {
	if got := Double(3); got != 6 {
		t.Errorf("Double(3) = %d, want 6", got)
	}
}
