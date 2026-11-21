// Package slow は、変異体ごとの判定に時間のかかるテストを持つ。実行の途中でプロセスを止める端から端までのテストが使う。
package slow

// Score は、変異箇所を多く持つ。
func Score(a, b, c, d int) int {
	s := 0
	if a > b {
		s += a - b
	}
	if c < d {
		s += d - c
	}
	return s*2 + a*b - c*d
}
