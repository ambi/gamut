//go:build !unix

package run

import "os/exec"

// isolate は、プロセスグループを持たない環境では何もしない。取り消しではテストバイナリだけを止める。
func isolate(*exec.Cmd) {}
