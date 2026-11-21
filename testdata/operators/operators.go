// Package operators has, for the mutators off by default, examples that end up KILLED and examples that end up LIVED.
package operators

import (
	"errors"
	"fmt"
)

// Turning acc += v into acc = v in Accumulate survives, because with the test's input acc = 0 the value does not change.
func Accumulate(acc, v int) int {
	acc += v
	return acc
}

// Turning Limit's 3 into 4 or 2 is detected, because the test checks the value.
func Limit() int { return 3 }

// Turning Retries' 5 into 6 or 4 survives, because the test does not check the value.
func Retries() int { return 5 }

// Turning Rate's 0.5 into 1.5 or -0.5 is detected, because the test checks the value.
func Rate() float64 { return 0.5 }

// Turning Ratio's 1.5 into 2.5 or 0.5 survives, because the test does not check the value.
func Ratio() float64 { return 1.5 }

// Emptying the body of Abs's if changes the value for the test's input -2, so it is detected.
func Abs(n int) int {
	if n < 0 {
		n = -n
	}
	return n
}

// Emptying the body of Note's if survives, because the test does not check the notification.
func Note(n int) int {
	if n > 0 {
		notified++
	}
	return n
}

// Emptying the body of Sign's else changes the value for the test's input 1, so it is detected.
func Sign(n int) int {
	s := 0
	if n < 0 {
		s = -1
	} else {
		s = 1
	}
	return s
}

// Emptying the body of Level's else survives, because the default value equals the else's value.
func Level(n int) string {
	l := "low"
	if n > 9 {
		l = "high"
	} else {
		l = "low"
	}
	return l
}

// Emptying the body of Name's case changes the value for the test's input 1, so it is detected.
func Name(n int) string {
	s := "?"
	switch n {
	case 1:
		s = "one"
	}
	return s
}

// Emptying the body of Kind's case survives, because the default value equals the case's value.
func Kind(n int) string {
	s := "even"
	switch n % 2 {
	case 0:
		s = "even"
	}
	return s
}

// ErrBase is the wrapped error.
var ErrBase = errors.New("base")

// Turning %w into %v in Wrap is detected, because the test checks with errors.Is.
func Wrap() error { return fmt.Errorf("wrap: %w", ErrBase) }

// Turning %w into %v in Annotate survives, because the test checks only the message.
func Annotate() error { return fmt.Errorf("annotate: %w", ErrBase) }

// Turning Check's return ErrBase into nil is detected with the test's input -1.
func Check(n int) error {
	if n < 0 {
		return ErrBase
	}
	return nil
}

// Turning Validate's return ErrBase into nil survives, because the test does not check the error.
func Validate(n int) error {
	if n > 100 {
		return ErrBase
	}
	return nil
}

// Turning a | b into a & b in Flags changes the value for the test's input (1, 2), so it is detected.
func Flags(a, b uint) uint { return a | b }

// Turning a & b into a | b in Mask survives, because the value does not change for the test's input (3, 3).
func Mask(a, b uint) uint { return a & b }

// Turning a << 1 into a >> 1 in Double changes the value for the test's input 2, so it is detected.
func Double(a uint) uint { return a << 1 }

// Turning sum += v into sum -= v in Total changes the value for the test's input, so it is detected.
func Total(vs []int) int {
	sum := 0
	for _, v := range vs {
		sum += v
	}
	return sum
}

// Turning n *= k into n /= k in Scaled survives, because the value does not change for the test's input k = 1.
func Scaled(n, k int) int {
	n *= k
	return n
}

// Turning f |= 1 into f &= 1 in SetBit changes the value for the test's input 2, so it is detected.
func SetBit(f uint) uint {
	f |= 1
	return f
}

// Turning a && b into a || b in Both changes the value for the test's input (true, false), so it is detected.
func Both(a, b bool) bool { return a && b }

// Turning a || b into a && b in Either survives, because the value does not change for the test's input (true, true).
func Either(a, b bool) bool { return a || b }

// Turning !open into open in Closed changes the value for the test's input false, so it is detected.
func Closed(open bool) bool { return !open }

// Turning !loud into loud in Muted survives, because the test does not check the return value.
func Muted(loud bool) bool { return !loud }

// Turning Enabled's true into false is detected, because the test checks the value.
func Enabled() bool { return true }

// Turning Verbose's false into true survives, because the test does not check the value.
func Verbose() bool { return false }

// Making Positive's if condition true or false changes the value for one of the test's inputs 1 and -1, so it is detected.
func Positive(n int) bool {
	if n > 0 {
		return true
	}
	return false
}

// Making SumAll's for condition false survives, because the test uses only empty input.
func SumAll(xs []int) int {
	s := 0
	for i := 0; i < len(xs); i++ {
		s += xs[i]
	}
	return s
}

// Replacing Area's body with a statement returning 0 is detected, because the test checks the value.
func Area(w, h int) int { return w * h }

// Replacing Describe's body with a statement returning the empty string survives, because the test does not check the value.
func Describe(n int) string { return "n" }

var recorded []string

func record(s string) { recorded = append(recorded, s) }

// Removing the call to record in Record is detected, because the test checks the record.
func Record(s string) { record(s) }

var notified int

func notify() { notified++ }

// Removing the call to notify in Notify survives, because the test does not check the notification.
func Notify() { notify() }

// Turning break into continue in FirstNegative returns the last negative number instead, detected with the test's input (1, -2, -3).
func FirstNegative(xs []int) int {
	found := 0
	for _, x := range xs {
		if x < 0 {
			found = x
			break
		}
	}
	return found
}

// Turning continue into break in CountPositive survives, because in the test's input (1, 2, 0) the only number at or below 0 is last.
func CountPositive(xs []int) int {
	n := 0
	for _, x := range xs {
		if x <= 0 {
			continue
		}
		n++
	}
	return n
}

// Turning f &^= 8 into f &= 8 in ClearHigh survives, because the value does not change for the test's input 0.
func ClearHigh(f uint) uint {
	f &^= 8
	return f
}
