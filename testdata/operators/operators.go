// Package operators は、既定では無効な変異演算子の、KILLED になる例と LIVED になる例を持つ。
package operators

// Flags の a | b を a & b にすると、テストの入力 (1, 2) で値が変わり、検出される。
func Flags(a, b uint) uint { return a | b }

// Mask の a & b を a | b にしても、テストの入力 (3, 3) では値が変わらないので生き残る。
func Mask(a, b uint) uint { return a & b }

// Double の a << 1 を a >> 1 にすると、テストの入力 2 で値が変わり、検出される。
func Double(a uint) uint { return a << 1 }
