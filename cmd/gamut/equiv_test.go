package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDetectEquivalent_MarksSurvivorsWithTheSameMachineCode checks that -detect-equivalent marks EQUIVALENT only survivors that compile to the original's code,
// leaves them out of the counts, and uses the cached comparison in the next run.
func TestDetectEquivalent_MarksSurvivorsWithTheSameMachineCode(t *testing.T) {
	dir := "../../testdata/equiv"
	plain, _ := mustGamut(t, "-no-cache", "-only", "ARITHMETIC_BASE", dir)
	for _, m := range plain {
		if m.Status != "Survived" {
			t.Fatalf("%s is %s without -detect-equivalent, want every mutant LIVED", m.ID, m.Status)
		}
	}
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	dir = copyModule(t, dir)
	for i, wantLog := range []string{"2 survivors compiled, 0 reused from the cache, 1 equivalent", "0 survivors compiled, 2 reused from the cache, 1 equivalent"} {
		if i > 0 {
			// Change only the modification time to skip the unchanged-rerun shortcut. The comparison keys come from contents, so they stay the same.
			later := time.Now().Add(time.Minute)
			if err := os.Chtimes(filepath.Join(dir, "equiv_test.go"), later, later); err != nil {
				t.Fatal(err)
			}
		}
		ms, log := mustGamut(t, "-cache", cachePath, "-only", "ARITHMETIC_BASE", "-detect-equivalent", dir)
		for _, m := range ms {
			want := "Survived"
			if m.scope() == "Scale" {
				want = "Ignored"
				if !strings.HasPrefix(m.Reason, "equivalent: ") {
					t.Errorf("run %d: %s has reason %q, want equivalent: ...", i+1, m.ID, m.Reason)
				}
			}
			if m.Status != want {
				t.Errorf("run %d: %s is %s, want %s", i+1, m.ID, m.Status, want)
			}
		}
		if !strings.Contains(log, wantLog) {
			t.Errorf("run %d: want %q in the log:\n%s", i+1, wantLog, log)
		}
	}
}
