package fixture

import (
	"log/slog"
	"time"
)

// Limit checks the classification of reach and infection.
func Limit(n, max int) int {
	if n > max { // no test tries n == max, so the boundary mutation is not infected
		return max
	}
	return n
}

// Sum checks the detection of INCREMENT_DECREMENT and ARITHMETIC_BASE.
func Sum(xs []int) int {
	total := 0
	for i := 0; i < len(xs); i++ {
		total = total + xs[i]
	}
	return total
}

// Negate checks the detection of INVERT_NEGATIVES.
func Negate(x int) int { return -x }

// Ready checks that the right side of short-circuit evaluation is not evaluated.
func Ready(name string, ok bool) bool {
	return name == "" || ok != false
}

// Unused is not called by tests.
func Unused(a, b int) int { return a * b }

// Timeout checks mutations in a constant context, which cannot be embedded in the schemata and are built standalone.
const Timeout = 3 * time.Second

// Logged checks that arguments of logging are not mutated.
func Logged(n int) int {
	slog.Info("value", "n", n+1)
	return n
}

// Loop checks timeouts: the mutation of i-- makes it never end.
func Loop(n int) int {
	c := 0
	for i := 0; i < n; i++ {
		c++
	}
	return c
}
