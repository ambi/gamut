package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/accept"
)

type acceptReport struct {
	Total    int `json:"mutants_total"`
	Accepted int `json:"mutants_accepted"`
	Files    []struct {
		Mutations []struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			Accepted string `json:"accepted"`
		} `json:"mutations"`
	} `json:"files"`
	Stale []struct {
		ID  string `json:"id"`
		Why string `json:"why"`
	} `json:"stale_accepted"`
}

func runAccepting(t *testing.T, args ...string) (acceptReport, string) {
	t.Helper()
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr strings.Builder
	full := append([]string{"-only", classicOnly, "-workers", "2", "-o", reportPath}, args...)
	if len(args) > 0 && args[0] == "accept" {
		full = append([]string{"accept", "-prune", "-only", classicOnly, "-workers", "2", "-o", reportPath}, args[1:]...)
	}
	if code := realMain(full, &stdout, &stderr); code != 0 {
		t.Fatalf("gamut %v exited %d:\n%s", full, code, stderr.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var rep acceptReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	return rep, stdout.String() + stderr.String()
}

func TestAccept_RecordsSurvivorsAndPrunesStaleOnes(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	write(t, dir, "go.mod", "module acc\n\ngo 1.27.4\n")
	write(t, dir, "acc.go", "package acc\n\nfunc Double(a int) int { return a * 2 }\n\nfunc Inc(a int) int { return a + 1 }\n")
	write(t, dir, "acc_test.go", "package acc\n\nimport \"testing\"\n\nfunc TestAll(t *testing.T) {\n\tif Double(2) < 0 || Inc(1) != 2 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n")

	first, _ := runAccepting(t, "-cache", cachePath, dir)
	var lived string
	for _, f := range first.Files {
		for _, m := range f.Mutations {
			if m.Status == "LIVED" && strings.Contains(m.ID, "::Double::") {
				lived = m.ID
			}
		}
	}
	if lived == "" {
		t.Fatalf("no LIVED mutant in Double: %+v", first.Files)
	}
	gone := "acc.go::ARITHMETIC_BASE::Gone::00deadbeef"
	var out strings.Builder
	if code := realMain([]string{"accept", "-reason", "equivalent: the sign is all callers read", "-dir", dir, lived, gone}, &out, io.Discard); code != 0 {
		t.Fatalf("accept exited %d", code)
	}
	if !strings.Contains(out.String(), "accepted 2 mutants") {
		t.Errorf("accept output %q", out.String())
	}

	// 受理した生き残りは、判定を保ったまま数から外れ、要約の直す一覧に並ばない。
	second, log := runAccepting(t, "-cache", cachePath, dir)
	if second.Total != first.Total-1 || second.Accepted != 1 {
		t.Errorf("total %d (was %d), accepted %d, want one fewer and 1", second.Total, first.Total, second.Accepted)
	}
	for _, f := range second.Files {
		for _, m := range f.Mutations {
			if m.ID == lived && (m.Status != "LIVED" || m.Accepted != "equivalent: the sign is all callers read") {
				t.Errorf("accepted mutant: status %s, accepted %q", m.Status, m.Accepted)
			}
		}
	}
	if strings.Contains(log, "not detected (") || !strings.Contains(log, ", 1 accepted") {
		t.Errorf("want the accepted survivor out of the to-do list and counted apart, log:\n%s", log)
	}
	if len(second.Stale) != 1 || second.Stale[0].ID != gone || second.Stale[0].Why != "missing" {
		t.Errorf("stale %+v, want only the gone ID as missing", second.Stale)
	}

	// テストを強めて検出されるようになると、消してよい受理として挙がる。
	write(t, dir, "acc_test.go", "package acc\n\nimport \"testing\"\n\nfunc TestAll(t *testing.T) {\n\tif Double(2) != 4 || Inc(1) != 2 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n")
	third, log := runAccepting(t, "-cache", cachePath, dir)
	if len(third.Stale) != 2 || !strings.Contains(log, "accepted but now detected (1)") || !strings.Contains(log, "accepted but gone (1)") {
		t.Errorf("stale %+v, want detected and gone, log:\n%s", third.Stale, log)
	}

	_, log = runAccepting(t, "accept", "-cache", cachePath, dir)
	if !strings.Contains(log, "removed 2 stale acceptances") {
		t.Errorf("want two acceptances pruned, log:\n%s", log)
	}
	left, err := accept.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left.Mutants) != 0 {
		t.Errorf("after -prune: %v, want none", left.Mutants)
	}
}

func TestAccept_RequiresAReasonAndIDs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module acc\n\ngo 1.27.4\n")
	for _, args := range [][]string{
		{"accept", "-dir", dir, "acc.go::ARITHMETIC_BASE::F::00"},
		{"accept", "-reason", "r", "-dir", dir},
		{"accept", "-reason", "r", "-dir", dir, "not-an-id"},
	} {
		if code := realMain(args, io.Discard, io.Discard); code != 1 {
			t.Errorf("%v: exit %d, want 1", args, code)
		}
	}
}
