// Package depth は、テスト関数から深さの違う呼び出しで到達する変異箇所を持つ。
package depth

func leaf(n int) int { return n + 1 }

func mid(n int) int { return leaf(n) }

// Top は、mid を通して leaf を呼ぶ。TestTop から leaf の変異箇所までの深さは 3 である。
func Top(n int) int { return mid(n) }
