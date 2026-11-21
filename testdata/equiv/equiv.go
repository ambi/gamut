// Package equiv は、コンパイル結果が元と同じになる変異体と、違う変異体を、どちらも生き残る形で持つ。
package equiv

// Scale の x * 1 を x / 1 にする変異体は、コンパイル結果が元と同じになる。
func Scale(x int) int { return x * 1 }

// Name の x + 2 を x - 2 にする変異体は、コンパイル結果が違うが、テストが弱いので生き残る。
func Name(x int) int { return x + 2 }
