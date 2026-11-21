// Package unrepeatable は、パッケージ変数を切り替えるテストを持つ。そのテストは、同じプロセスで 2 回実行すると失敗し、
// 前の実行の状態が残ったプロセスでは、切り替えを消した変異体でも通りうる。
package unrepeatable

var on bool

func toggle() { on = !on }

// Switch は on を切り替え、times に 1 を足して返す。テストは戻り値を確かめないので、足し算の変異体は生き残る。
func Switch(times int) int {
	toggle()
	return times + 1
}

// Twice は、繰り返し実行できるテストが確かめる。
func Twice(n int) int { return n * 2 }
