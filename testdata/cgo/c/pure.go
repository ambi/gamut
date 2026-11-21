package c

// Double は、cgo の関数を呼ぶ。このファイルは import "C" を持たないので変異する。
func Double(n int) int { return Add(n, n) }

// Inc は n に 1 を足す。
func Inc(n int) int { return n + 1 }
