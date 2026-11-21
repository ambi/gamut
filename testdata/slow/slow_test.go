package slow

import (
	"testing"
	"time"
)

// TestScore waits 200 milliseconds each time before checking, so judging takes long.
func TestScore(t *testing.T) {
	time.Sleep(200 * time.Millisecond)
	if Score(5, 2, 1, 4) != 3*2+3*2+10-4 {
		t.Fatal("wrong")
	}
}
