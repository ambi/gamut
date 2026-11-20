package orphan

// Ready は、変異で偽になると、テストが子プロセスを起動して待ち続ける。
func Ready(n int) bool { return n > 0 }
