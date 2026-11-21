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

// TestCheckpoint_SurvivesSIGKILL checks that after a SIGKILL midway, verdicts finished up to the last checkpoint are reused by the next run.
// It also checks that with checkpointing off nothing remains.
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
			// Stop once two or more verdicts are recorded or time runs out. With checkpointing off, wait for judging to progress before stopping.
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

			// The cache's environment key includes the contents of the gamut executable, so rerun with the same executable.
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

// TestCheckpoint_KeepsVerdicts checks that checkpointing does not change verdicts.
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

// recorded returns the number of verdicts recorded in the cache file, or 0 when the file is missing or unreadable.
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
