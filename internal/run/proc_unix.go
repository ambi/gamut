//go:build unix

package run

import (
	"os/exec"
	"syscall"
)

// isolate starts cmd in a new process group, and on cancellation sends SIGKILL to the whole group.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
