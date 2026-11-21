// Package plain は cgo を使わない。cgo のパッケージを飛ばしても判定する。
package plain

// Half は n を 2 で割る。
func Half(n int) int { return n / 2 }
