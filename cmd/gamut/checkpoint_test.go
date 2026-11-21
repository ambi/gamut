package main

import (
	"encoding/json"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestCheckpoint_SurvivesSIGKILL は、実行の途中で SIGKILL を受けても、書き出しの間隔までに終わった判定が次の実行で再利用されることを確かめる。
// 書き出しを止めると、何も残らないことも確かめる。
func TestCheckpoint_SurvivesSIGKILL(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "gamut")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build gamut: %v\n%s", err, out)
	}
	for _, tc := range []struct {
		interval string
		reused   bool
	}{{"100ms", true}, {"0", false}} {
		t.Run(tc.interval, func(t *testing.T) {
			dir := copyModule(t, "../../testdata/slow")
			cachePath := filepath.Join(t.TempDir(), "cache.json")
			cmd := exec.Command(bin, "-q", "-workers", "1", "-only", "ARITHMETIC_BASE", "-cache", cachePath, "-checkpoint-interval", tc.interval, dir)
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			// 判定が 2 体以上記録されるか、時間が過ぎたら止める。書き出しを止めたときは、判定が進むのを待ってから止める。
			deadline := time.Now().Add(20 * time.Second)
			for time.Now().Before(deadline) {
				if tc.reused && recorded(cachePath) >= 2 {
					break
				}
				if !tc.reused && time.Since(deadline.Add(-20*time.Second)) > 3*time.Second {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			_ = cmd.Process.Signal(syscall.SIGKILL)
			_ = cmd.Wait()

			// キャッシュの環境の鍵は gamut の実行ファイルの中身を含むので、同じ実行ファイルで実行し直す。
			var stderr strings.Builder
			rerun := exec.Command(bin, "-workers", "1", "-only", "ARITHMETIC_BASE", "-cache", cachePath, dir)
			rerun.Stdout, rerun.Stderr = io.Discard, &stderr
			if err := rerun.Run(); err != nil {
				t.Fatalf("rerun: %v\n%s", err, stderr.String())
			}
			m := regexp.MustCompile(`cache: (\d+) mutants reused`).FindStringSubmatch(stderr.String())
			if m == nil {
				t.Fatalf("no reuse line:\n%s", stderr.String())
			}
			n, _ := strconv.Atoi(m[1])
			if got := n > 0; got != tc.reused {
				t.Errorf("interval %s: %d mutants reused after SIGKILL, want reuse %t", tc.interval, n, tc.reused)
			}
		})
	}
}

// TestCheckpoint_KeepsVerdicts は、書き出しの有無で判定が変わらないことを確かめる。
func TestCheckpoint_KeepsVerdicts(t *testing.T) {
	verdicts := func(interval string) map[string]string {
		ms, _ := mustGamut(t, "-cache", filepath.Join(t.TempDir(), "cache.json"), "-checkpoint-interval", interval, "../../testdata/fixture")
		out := map[string]string{}
		for _, m := range ms {
			out[m.ID] = m.Status
		}
		return out
	}
	with, without := verdicts("5ms"), verdicts("0")
	if len(with) == 0 || !maps.Equal(with, without) {
		t.Errorf("verdicts with checkpoints %v, without %v", with, without)
	}
}

// recorded は、キャッシュファイルに記録された判定の数を返す。ファイルがないか読めなければ 0 を返す。
func recorded(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var f struct {
		Mutants map[string]json.RawMessage `json:"mutants"`
	}
	if json.Unmarshal(data, &f) != nil {
		return 0
	}
	return len(f.Mutants)
}
