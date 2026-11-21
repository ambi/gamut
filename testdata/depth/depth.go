// Package depth has mutation sites reached from test functions through calls of different depths.
package depth

func leaf(n int) int { return n + 1 }

func mid(n int) int { return leaf(n) }

// Top calls leaf through mid. The depth from TestTop to leaf's mutation site is 3.
func Top(n int) int { return mid(n) }
