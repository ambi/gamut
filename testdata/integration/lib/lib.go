// Package lib は、呼び出し元のパッケージのテストでだけ検出できる変異体を持つ。
package lib

// Add は a と b の和を返す。
func Add(a, b int) int {
	return a + b
}

// Triple は v の 3 倍を返す。lib のテストは呼ばない。
func Triple(v int) int {
	return v * 3
}

// Unit は、unrelated が参照するだけの定数である。
const Unit = "m"
