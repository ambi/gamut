// Package c has files that use cgo and files that do not. Only mutants of the files that do not are judged.
package c

// int add(int a, int b) { return a + b; }
import "C"

// Add adds with a C function. This file has import "C", so it is not mutated.
func Add(a, b int) int { return int(C.add(C.int(a), C.int(b))) - 0 }
