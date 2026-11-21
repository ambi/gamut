package slow

import (
	"testing"
	"time"
)

// TestScore は、判定に時間がかかるよう、毎回 200 ミリ秒待ってから確かめる。
func TestScore(t *testing.T) {
	time.Sleep(200 * time.Millisecond)
	if Score(5, 2, 1, 4) != 3*2+3*2+10-4 {
		t.Fatal("wrong")
	}
}
