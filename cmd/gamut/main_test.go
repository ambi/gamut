package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ambi/gamut/internal/run"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errors.New("build failed"), 1},
		{fmt.Errorf("wrapped: %w", run.ErrSelfCheck), 4},
		{fmt.Errorf("wrapped: %w", run.ErrBaseline), 4},
	}
	for _, c := range cases {
		if got := exitCode(c.err); got != c.want {
			t.Errorf("exitCode(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}
