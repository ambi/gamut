package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMaxDepth checks that -max-depth removes from the reaching tests those that reach a mutation site only through deep calls.
// It also checks, in a run restoring coverage records from the cache, that depth counts from subtest functions and that the removed count is reported.
func TestMaxDepth(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		tests    []string
		excluded int
	}{
		{nil, []string{"TestLeaf", "TestTop"}, 0},
		{[]string{"-max-depth", "2"}, []string{"TestLeaf"}, 1},
		{[]string{"-max-depth", "3"}, []string{"TestLeaf", "TestTop"}, 0},
	} {
		t.Run(strings.Join(append([]string{"depth"}, tc.args...), " "), func(t *testing.T) {
			dir := copyModule(t, "../../testdata/depth")
			cachePath := filepath.Join(t.TempDir(), "cache.json")
			for _, run := range []string{"measuring", "restoring"} {
				if run == "restoring" {
					// Change only the modification time to skip the unchanged-rerun shortcut. Coverage records are restored.
					later := time.Now().Add(time.Minute)
					if err := os.Chtimes(filepath.Join(dir, "depth_test.go"), later, later); err != nil {
						t.Fatal(err)
					}
				}
				reportPath := filepath.Join(t.TempDir(), "r.json")
				var stdout, stderr strings.Builder
				args := slices.Concat([]string{"-only", "ARITHMETIC_BASE", "-o", reportPath, "-cache", cachePath}, tc.args, []string{dir})
				if code := realMain(args, &stdout, &stderr); code != 0 {
					t.Fatalf("%s: exit %d:\n%s", run, code, stderr.String())
				}
				if run == "restoring" && !strings.Contains(stderr.String(), "2 restored from the cache") {
					t.Errorf("coverage was not restored:\n%s", stderr.String())
				}
				rep := readMTE(t, reportPath)
				var tests []string
				for _, m := range rep.mutants() {
					tests = append(tests, m.Tests...)
				}
				slices.Sort(tests)
				if !slices.Equal(tests, tc.tests) {
					t.Errorf("%s: reached by %v, want %v", run, tests, tc.tests)
				}
				excluded := 0
				if rep.Gamut.MaxDepthExcluded != nil {
					excluded = rep.Gamut.MaxDepthExcluded.Tests
				}
				if excluded != tc.excluded {
					t.Errorf("%s: maxDepthExcluded.tests %d, want %d", run, excluded, tc.excluded)
				}
				if tc.excluded > 0 && !strings.Contains(stdout.String(), "beyond -max-depth") {
					t.Errorf("%s: the summary does not mention -max-depth:\n%s", run, stdout.String())
				}
			}
		})
	}
}
