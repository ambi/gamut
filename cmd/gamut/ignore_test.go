package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestIgnore_ReportsCommentedMutantsApart は、無効化コメントで止めた変異体が files[].mutations に入らず、
// 理由とともに ignored に入ること、数えないこと、理由のないコメントを警告することを確かめる。
func TestIgnore_ReportsCommentedMutantsApart(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var stderr strings.Builder
	if code := realMain([]string{"-no-cache", "-workers", "2", "-o", reportPath, "../../testdata/ignore"}, io.Discard, &stderr); code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Total   int `json:"mutants_total"`
		Ignored int `json:"mutants_ignored"`
		Files   []struct {
			Mutations []mutation `json:"mutations"`
		} `json:"files"`
		IgnoredList []struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Line   int    `json:"line"`
			Reason string `json:"reason"`
		} `json:"ignored"`
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	var judged []string
	for _, f := range rep.Files {
		for _, m := range f.Mutations {
			judged = append(judged, m.scope()+" "+m.Type)
		}
	}
	slices.Sort(judged)
	wantJudged := []string{"Ranged CONDITIONALS_BOUNDARY", "Ranged CONDITIONALS_NEGATION", "Sum ARITHMETIC_BASE"}
	if !slices.Equal(judged, wantJudged) {
		t.Errorf("judged %v, want %v", judged, wantJudged)
	}
	reasons := map[string]int{}
	for _, ig := range rep.IgnoredList {
		reasons[mutation{ID: ig.ID}.scope()+": "+ig.Reason]++
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
	if rep.Ignored != 9 || rep.Total != 3 {
		t.Errorf("mutants_ignored %d, mutants_total %d, want 9 and 3", rep.Ignored, rep.Total)
	}
	if !strings.Contains(stderr.String(), "warning: ignore.go:33: //gamut:ignore has no reason") {
		t.Errorf("want a warning for the comment without a reason, log:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "9 ignored by comments") {
		t.Errorf("want the ignored count in the summary, log:\n%s", stderr.String())
	}

	// 止めた変異体を -mutant で指定したら、コメントの位置を示して終了コード 1 にする。
	_, out, code := gamut(t, "-no-cache", "-mutant", rep.IgnoredList[0].ID, "../../testdata/ignore")
	if code != 1 || !strings.Contains(out, "//gamut:ignore comment covers") {
		t.Errorf("-mutant on an ignored mutant: exit %d, want 1 with the comment position:\n%s", code, out)
	}
}
