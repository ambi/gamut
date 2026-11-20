//go:build unix

package run

import (
	"os/exec"
	"syscall"
)

// isolate は、cmd を新しいプロセスグループで起動し、取り消しではグループ全体へ SIGKILL を送る。
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
