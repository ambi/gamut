package orphan

import (
	"os"
	"os/exec"
	"testing"
)

// TestReady, when Ready returns false, starts a child process that writes its pid to ORPHAN_PIDFILE and sleeps, and waits for it to exit.
// Stopping only the test binary on a timeout would leave this child process behind.
func TestReady(t *testing.T) {
	if Ready(1) {
		return
	}
	cmd := exec.Command("sh", "-c", `echo $$ > "$ORPHAN_PIDFILE"; exec sleep 300`)
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}
