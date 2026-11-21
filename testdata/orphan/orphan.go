package orphan

// Ready, when a mutation makes it false, makes the test start a child process and wait for it forever.
func Ready(n int) bool { return n > 0 }
