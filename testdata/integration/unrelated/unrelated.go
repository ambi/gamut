// Package unrelated は lib を import するが、変異箇所を通らない。
package unrelated

import "integration/lib"

// Meters は単位を付けた文字列を返す。
func Meters(s string) string {
	return s + lib.Unit
}
