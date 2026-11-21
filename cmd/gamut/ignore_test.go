package main

import (
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestIgnore_ReportsCommentedMutantsApart checks that mutants stopped by disable comments are listed in the report as Ignored with the comment's reason,
// are not counted, and that comments without a reason get a warning.
func TestIgnore_ReportsCommentedMutantsApart(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var stderr strings.Builder
	if code := realMain([]string{"-no-cache", "-only", classicOnly, "-workers", "2", "-o", reportPath, "../../testdata/ignore"}, io.Discard, &stderr); code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr.String())
	}
	// Stopped mutants are listed as Ignored with the comment's reason.
	rep := readMTE(t, reportPath)
	var judged, ignoredIDs []string
	reasons := map[string]int{}
	for _, m := range rep.mutants() {
		if reason, ok := strings.CutPrefix(m.Reason, "ignored by comment: "); ok && m.Status == "Ignored" {
			reasons[m.scope()+": "+reason]++
			ignoredIDs = append(ignoredIDs, m.ID)
			continue
		}
		judged = append(judged, m.scope()+" "+m.Type)
	}
	slices.Sort(judged)
	wantJudged := []string{"Ranged CONDITIONALS_BOUNDARY", "Ranged CONDITIONALS_NEGATION", "Sum ARITHMETIC_BASE"}
	if !slices.Equal(judged, wantJudged) {
		t.Errorf("judged %v, want %v", judged, wantJudged)
	}
	wantReasons := map[string]int{
		"Cached: the product is only a cache key":        1,
		"Fast: a fast path with the same result as Slow": 5,
		"Ranged: defensive arithmetic":                   2,
		"NoReason: ":                                     1,
	}
	for k, n := range wantReasons {
		if reasons[k] != n {
			t.Errorf("ignored %q: %d mutants, want %d (all: %v)", k, reasons[k], n, reasons)
		}
	}
	if rep.Gamut.Counts.Ignored != 9 || rep.Gamut.Counts.Total != 3 {
		t.Errorf("ignored %d, total %d, want 9 and 3", rep.Gamut.Counts.Ignored, rep.Gamut.Counts.Total)
	}
	if !strings.Contains(stderr.String(), "warning: ignore.go:33: //gamut:ignore has no reason") {
		t.Errorf("want a warning for the comment without a reason, log:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "9 ignored by comments") {
		t.Errorf("want the ignored count in the summary, log:\n%s", stderr.String())
	}

	// Selecting a stopped mutant with -mutant points at the comment and exits with code 1.
	_, out, code := gamut(t, "-no-cache", "-only", classicOnly, "-mutant", ignoredIDs[0], "../../testdata/ignore")
	if code != 1 || !strings.Contains(out, "//gamut:ignore comment covers") {
		t.Errorf("-mutant on an ignored mutant: exit %d, want 1 with the comment position:\n%s", code, out)
	}
}
