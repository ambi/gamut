// Package ignore は、無効化コメントの各形式を持つ。端から端までのテストが、止めた変異体だけが報告の ignored に入ることを確かめる。
package ignore

// Sum は、止めない変異体を持つ。
func Sum(a, b int) int { return a + b }

// Cached は、行末のコメントで止める。
func Cached(a, b int) int {
	return a * b //gamut:ignore -- the product is only a cache key
}

// Fast は、関数の宣言の直前のコメントで、関数全体を止める。
//
//gamut:ignore -- a fast path with the same result as Slow
func Fast(a, b int) bool {
	if a < b {
		return true
	}
	return a-b < 0
}

// Ranged は、範囲で ARITHMETIC_BASE だけを止める。比較は止めない。
func Ranged(a, b int) bool {
	//gamut:ignore-start ARITHMETIC_BASE -- defensive arithmetic
	x := a + b
	y := a - b
	//gamut:ignore-end
	return x > y
}

// NoReason は、理由のないコメントを持つ。止めるが警告する。
func NoReason(a, b int) int {
	return a - b //gamut:ignore
}
