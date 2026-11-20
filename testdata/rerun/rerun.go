// Package rerun は、生き残りを埋めるテストを足してから、その変異体だけを再実行することを確かめる固定モジュールである。
package rerun

// IsAdult は、age が 18 以上かを返す。
func IsAdult(age int) bool {
	return age >= 18
}

// Half は v の半分を返す。
func Half(v int) int {
	return v / 2
}
