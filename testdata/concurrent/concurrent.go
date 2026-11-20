package concurrent

// Scale と Shift は、並行に動くテストとゴルーチンからだけ呼ばれる。
func Scale(x int) int { return x * 3 }

func Shift(x int) int { return x - 7 }
