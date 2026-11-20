package lib

import "testing"

// TestAdd は Add を呼ぶが結果を確かめないので、Add の変異体は lib のテストでは生き残る。
func TestAdd(t *testing.T) {
	_ = Add(1, 2)
}
