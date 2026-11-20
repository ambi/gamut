package orphan

import (
	"os"
	"os/exec"
	"testing"
)

// TestReady は、Ready が偽を返すと、自分の pid を ORPHAN_PIDFILE に書いて眠る子プロセスを起動し、その終了を待つ。
// 時間切れでテストバイナリだけを止めると、この子プロセスが残る。
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
