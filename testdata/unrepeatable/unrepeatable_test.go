package unrepeatable

import "testing"

// TestSwitch は、on を戻さずに確かめる。同じプロセスで 2 回目に実行すると、on が偽に戻って失敗する。
func TestSwitch(t *testing.T) {
	Switch(1)
	if !on {
		t.Fatal("not switched on")
	}
}

func TestTwice(t *testing.T) {
	if Twice(3) != 6 {
		t.Fatal("wrong")
	}
}
