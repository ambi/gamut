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
