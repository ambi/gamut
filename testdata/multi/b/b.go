// Package b は、対象パッケージ a を import する。
package b

import "multi/a"

// Scale は v を k 倍し、負なら 0 にする。
func Scale(v, k int) int {
	return a.Max(v*k, 0)
}

// Neg は v の符号を反転する。
func Neg(v int) int {
	return -v
}
