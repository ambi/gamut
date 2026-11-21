//go:build !unix

package run

import "os/exec"

// isolate does nothing where there are no process groups. Cancellation stops only the test binary.
func isolate(*exec.Cmd) {}
