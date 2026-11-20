// Package app は lib を呼ぶ。
package app

import "integration/lib"

// Sum3 は 3 つの値の和を返す。
func Sum3(a, b, c int) int {
	return lib.Add(lib.Add(a, b), c)
}

// Nine は 3 の 3 倍を返す。
func Nine() int {
	return lib.Triple(3)
}
