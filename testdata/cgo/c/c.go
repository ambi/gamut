// Package c は、cgo を使うファイルと使わないファイルを持つ。使わないファイルの変異体だけを判定する。
package c

// int add(int a, int b) { return a + b; }
import "C"

// Add は C の関数で足す。このファイルは import "C" を持つので変異しない。
func Add(a, b int) int { return int(C.add(C.int(a), C.int(b))) - 0 }
