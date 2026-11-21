// Package operators は、既定では無効な変異演算子の、KILLED になる例と LIVED になる例を持つ。
package operators

import (
	"errors"
	"fmt"
)

// Accumulate の acc += v を acc = v にしても、テストの入力 acc = 0 では値が変わらないので生き残る。
func Accumulate(acc, v int) int {
	acc += v
	return acc
}

// Limit の 3 を 4 にしても 2 にしても、テストが値を確かめるので検出される。
func Limit() int { return 3 }

// Retries の 5 を 6 にしても 4 にしても、テストが値を確かめないので生き残る。
func Retries() int { return 5 }

// Rate の 0.5 を 1.5 にしても -0.5 にしても、テストが値を確かめるので検出される。
func Rate() float64 { return 0.5 }

// Ratio の 1.5 を 2.5 にしても 0.5 にしても、テストが値を確かめないので生き残る。
func Ratio() float64 { return 1.5 }

// Abs の if の本体を空にすると、テストの入力 -2 で値が変わり、検出される。
func Abs(n int) int {
	if n < 0 {
		n = -n
	}
	return n
}

// Note の if の本体を空にしても、テストが通知を確かめないので生き残る。
func Note(n int) int {
	if n > 0 {
		notified++
	}
	return n
}

// Sign の else の本体を空にすると、テストの入力 1 で値が変わり、検出される。
func Sign(n int) int {
	s := 0
	if n < 0 {
		s = -1
	} else {
		s = 1
	}
	return s
}

// Level の else の本体を空にしても、既定の値が else の値と同じなので生き残る。
func Level(n int) string {
	l := "low"
	if n > 9 {
		l = "high"
	} else {
		l = "low"
	}
	return l
}

// Name の case の本体を空にすると、テストの入力 1 で値が変わり、検出される。
func Name(n int) string {
	s := "?"
	switch n {
	case 1:
		s = "one"
	}
	return s
}

// Kind の case の本体を空にしても、既定の値が case の値と同じなので生き残る。
func Kind(n int) string {
	s := "even"
	switch n % 2 {
	case 0:
		s = "even"
	}
	return s
}

// ErrBase は、包まれるエラーである。
var ErrBase = errors.New("base")

// Wrap の %w を %v にすると、テストが errors.Is で確かめるので検出される。
func Wrap() error { return fmt.Errorf("wrap: %w", ErrBase) }

// Annotate の %w を %v にしても、テストがメッセージしか確かめないので生き残る。
func Annotate() error { return fmt.Errorf("annotate: %w", ErrBase) }

// Check の return ErrBase を nil にすると、テストの入力 -1 で検出される。
func Check(n int) error {
	if n < 0 {
		return ErrBase
	}
	return nil
}

// Validate の return ErrBase を nil にしても、テストがエラーを確かめないので生き残る。
func Validate(n int) error {
	if n > 100 {
		return ErrBase
	}
	return nil
}

// Flags の a | b を a & b にすると、テストの入力 (1, 2) で値が変わり、検出される。
func Flags(a, b uint) uint { return a | b }

// Mask の a & b を a | b にしても、テストの入力 (3, 3) では値が変わらないので生き残る。
func Mask(a, b uint) uint { return a & b }

// Double の a << 1 を a >> 1 にすると、テストの入力 2 で値が変わり、検出される。
func Double(a uint) uint { return a << 1 }

// Total の sum += v を sum -= v にすると、テストの入力で値が変わり、検出される。
func Total(vs []int) int {
	sum := 0
	for _, v := range vs {
		sum += v
	}
	return sum
}

// Scaled の n *= k を n /= k にしても、テストの入力 k = 1 では値が変わらないので生き残る。
func Scaled(n, k int) int {
	n *= k
	return n
}

// SetBit の f |= 1 を f &= 1 にすると、テストの入力 2 で値が変わり、検出される。
func SetBit(f uint) uint {
	f |= 1
	return f
}

// Both の a && b を a || b にすると、テストの入力 (true, false) で値が変わり、検出される。
func Both(a, b bool) bool { return a && b }

// Either の a || b を a && b にしても、テストの入力 (true, true) では値が変わらないので生き残る。
func Either(a, b bool) bool { return a || b }

// Closed の !open を open にすると、テストの入力 false で値が変わり、検出される。
func Closed(open bool) bool { return !open }

// Muted の !loud を loud にしても、テストが戻り値を確かめないので生き残る。
func Muted(loud bool) bool { return !loud }

// Enabled の true を false にすると、テストが値を確かめるので検出される。
func Enabled() bool { return true }

// Verbose の false を true にしても、テストが値を確かめないので生き残る。
func Verbose() bool { return false }

// Positive の if の条件を true にしても false にしても、テストの入力 1 と -1 のどちらかで値が変わり、検出される。
func Positive(n int) bool {
	if n > 0 {
		return true
	}
	return false
}

// SumAll の for の条件を false にしても、テストが空の入力しか使わないので生き残る。
func SumAll(xs []int) int {
	s := 0
	for i := 0; i < len(xs); i++ {
		s += xs[i]
	}
	return s
}

// Area の本体を 0 を返す文にすると、テストが値を確かめるので検出される。
func Area(w, h int) int { return w * h }

// Describe の本体を空の文字列を返す文にしても、テストが値を確かめないので生き残る。
func Describe(n int) string { return "n" }

var recorded []string

func record(s string) { recorded = append(recorded, s) }

// Record の record の呼び出しを消すと、テストが記録を確かめるので検出される。
func Record(s string) { record(s) }

var notified int

func notify() { notified++ }

// Notify の notify の呼び出しを消しても、テストが通知を確かめないので生き残る。
func Notify() { notify() }

// FirstNegative の break を continue にすると最後の負の数を返すようになり、テストの入力 (1, -2, -3) で検出される。
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

// CountPositive の continue を break にしても、テストの入力 (1, 2, 0) では 0 以下の数が最後にしかないので生き残る。
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

// ClearHigh の f &^= 8 を f &= 8 にしても、テストの入力 0 では値が変わらないので生き残る。
func ClearHigh(f uint) uint {
	f &^= 8
	return f
}
