//go:build extra

package tags

// Double は extra タグを渡したときだけ列挙される。
func Double(x int) int { return x * 2 }
