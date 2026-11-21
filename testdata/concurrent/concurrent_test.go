package concurrent

import (
	"sync"
	"testing"
)

// each calls fn from many goroutines at once and fails on any unexpected result.
func each(t *testing.T, fn func(int) int, want func(int) int) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan int, 64)
	for i := range 64 {
		wg.Go(func() {
			if fn(i) != want(i) {
				errs <- i
			}
		})
	}
	wg.Wait()
	close(errs)
	for i := range errs {
		t.Errorf("wrong result for %d", i)
	}
}

func TestScale(t *testing.T) {
	t.Parallel()
	each(t, Scale, func(i int) int { return i * 3 })
}

func TestShift(t *testing.T) {
	t.Parallel()
	each(t, Shift, func(i int) int { return i - 7 })
}
