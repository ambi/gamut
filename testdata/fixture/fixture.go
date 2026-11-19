package fixture

import (
	"log/slog"
	"time"
)

// Limit は、到達と感染の分類を確かめるための関数である。
func Limit(n, max int) int {
	if n > max { // n == max を試すテストがないので境界の変異は感染しない
		return max
	}
	return n
}

// Sum は INCREMENT_DECREMENT と ARITHMETIC_BASE の検出を確かめる。
func Sum(xs []int) int {
	total := 0
	for i := 0; i < len(xs); i++ {
		total = total + xs[i]
	}
	return total
}

// Negate は INVERT_NEGATIVES の検出を確かめる。
func Negate(x int) int { return -x }

// Ready は短絡評価の右辺が評価されないことを確かめる。
func Ready(name string, ok bool) bool {
	return name == "" || ok != false
}

// Unused はテストから呼ばれない。
func Unused(a, b int) int { return a * b }

// Timeout は定数の文脈の変異を確かめる。schemata に埋め込めず、個別にビルドする。
const Timeout = 3 * time.Second

// Logged はログ出力の引数を変異しないことを確かめる。
func Logged(n int) int {
	slog.Info("value", "n", n+1)
	return n
}

// Loop は時間切れを確かめる。i-- の変異で終わらなくなる。
func Loop(n int) int {
	c := 0
	for i := 0; i < n; i++ {
		c++
	}
	return c
}
