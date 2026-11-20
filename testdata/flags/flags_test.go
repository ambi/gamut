package flags

import "testing"

// TestHalf は -short なしでは失敗する。テストのフラグが渡らないと基準のテストが壊れる。
func TestHalf(t *testing.T) {
	if !testing.Short() {
		t.Fatal("this test needs -short")
	}
	if Half(8) != 4 {
		t.Fatal("Half")
	}
}
