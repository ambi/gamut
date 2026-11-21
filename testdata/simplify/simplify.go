// Package simplify は、不要な分岐と、結果を誰も見ない呼び出しを持つ。-reporter simplify が候補として挙げる。
package simplify

var touched int

func touch() { touched++ }

// Abs は絶対値を返す。n == 0 の分岐は、最後の return と同じ値を返すので要らない。
func Abs(n int) int {
	touch()
	if n < 0 {
		return -n
	}
	if n == 0 {
		return 0
	}
	return n
}

// Reset はテストが呼ばない。その文の削除は候補ではなく、到達しない数に入る。
func Reset() { touch() }
