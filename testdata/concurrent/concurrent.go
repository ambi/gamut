package concurrent

// Scale and Shift are called only from concurrently running tests and goroutines.
func Scale(x int) int { return x * 3 }

func Shift(x int) int { return x - 7 }
