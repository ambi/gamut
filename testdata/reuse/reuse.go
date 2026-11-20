// Package reuse は、判定の再利用を確かめる固定モジュールである。
// Score は補助関数 Clamp を呼び、Twice はほかの宣言を参照しない。
package reuse

// Clamp は v を lo から hi に収める。
func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Score は、0 以上の v を 2 倍して 100 で頭打ちにし、負の v には 0 を返す。
func Score(v int) int {
	if v >= 0 {
		return Clamp(v*2, 0, 100)
	}
	return 0
}

// Twice は v の 2 倍を返す。
func Twice(v int) int {
	return v + v
}
