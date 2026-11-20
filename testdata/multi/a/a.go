// Package a は、b が import する対象パッケージである。
package a

// Max は x と y の大きい方を返す。
func Max(x, y int) int {
	if x > y {
		return x
	}
	return y
}
