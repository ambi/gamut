// Package operators は、既定では無効な変異演算子の、KILLED になる例と LIVED になる例を持つ。
package operators

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
