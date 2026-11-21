package main

import (
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShard_MergeEqualsOneRun checks that the report and cache merged from two shards match those of one run.
func TestShard_MergeEqualsOneRun(t *testing.T) {
	dir := "../../testdata/fixture"
	whole, _ := mustGamut(t, "-no-cache", dir)
	want := statuses(whole)
	tmp := t.TempDir()
	var caches []string
	covered := map[string]bool{}
	for _, shard := range []string{"1/2", "2/2"} {
		n, _, _ := strings.Cut(shard, "/")
		reportPath, cachePath := filepath.Join(tmp, "r"+n+".json"), filepath.Join(tmp, "c"+n+".json")
		var stderr strings.Builder
		if code := realMain([]string{"-shard", shard, "-o", reportPath, "-cache", cachePath, dir}, &strings.Builder{}, &stderr); code != 0 {
			t.Fatalf("shard %s: exit %d:\n%s", shard, code, stderr.String())
		}
		ms := readReport(t, reportPath)
		judged := 0
		for _, m := range ms {
			if m.Status == "Pending" {
				continue
			}
			judged++
			if covered[m.ID] {
				t.Errorf("%s is judged in both shards", m.ID)
			}
			covered[m.ID] = true
		}
		if judged == 0 || judged == len(ms) {
			t.Errorf("shard %s judged %d of %d mutants; want a part", shard, judged, len(ms))
		}
		caches = append(caches, cachePath)
	}
	// Only the shards' caches are merged; their reports are not read.
	merged, mergedCache := filepath.Join(tmp, "merged.json"), filepath.Join(tmp, "merged-cache.json")
	var stdout, stderr strings.Builder
	args := []string{"merge", "-o", merged, "-cache-out", mergedCache, caches[0], caches[1]}
	if code := realMain(args, &stdout, &stderr); code != 0 {
		t.Fatalf("merge: exit %d:\n%s", code, stderr.String())
	}
	if got := statuses(readReport(t, merged)); !maps.Equal(got, want) {
		t.Errorf("merged %v, one run %v", got, want)
	}
	if !strings.Contains(stdout.String(), "killed") {
		t.Errorf("merge prints no summary:\n%s", stdout.String())
	}
	// Running again with the merged cache reuses every verdict.
	_, log := mustGamut(t, "-cache", mergedCache, dir)
	if !strings.Contains(log, " 0 to execute") {
		t.Errorf("the merged cache is not reused:\n%s", log)
	}
	// A cache without executed verdicts cannot be merged.
	empty := filepath.Join(tmp, "empty.json")
	if err := os.WriteFile(empty, []byte(`{"version":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := realMain([]string{"merge", empty}, io.Discard, &stderr); code != exitError || !strings.Contains(stderr.String(), "no report of a run") {
		t.Errorf("merging a cache without a run: exit %d:\n%s", code, stderr.String())
	}
	for _, bad := range [][]string{{"-shard", "3/2"}, {"-shard", "0/2"}, {"-shard", "x"}} {
		if _, _, code := gamut(t, append(append([]string{"-no-cache"}, bad...), dir)...); code != exitError {
			t.Errorf("%v: exit %d, want %d", bad, code, exitError)
		}
	}
}

// TestTimeBudget_LeavesTheRestPending checks that past the budget no new mutant starts, the rest are PENDING and not cached, and the next run judges them.
func TestTimeBudget_LeavesTheRestPending(t *testing.T) {
	dir := "../../testdata/fixture"
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	ms, log := mustGamut(t, "-cache", cachePath, "-time-budget", "1ns", dir)
	pending := 0
	for _, m := range ms {
		if m.Status == "Pending" {
			pending++
		}
		if m.Status == "Killed" || m.Status == "Survived" && m.Reason == "not detected" {
			t.Errorf("%s was executed after the budget ran out: %s", m.ID, m.Status)
		}
	}
	if pending == 0 || !strings.Contains(log, "time budget") {
		t.Fatalf("%d pending; log:\n%s", pending, log)
	}
	ms, log = mustGamut(t, "-cache", cachePath, dir)
	for _, m := range ms {
		if m.Status == "Pending" {
			t.Errorf("%s is still pending", m.ID)
		}
	}
	if !strings.Contains(log, " 0 mutants reused") {
		t.Errorf("verdicts skipped by the budget were cached:\n%s", log)
	}
}
