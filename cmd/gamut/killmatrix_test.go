package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestKillMatrix は、-kill-matrix が、検出したテストをすべて記録し、検出しないテスト、包含されるテスト、重複するテストを報告することを確かめる。
// panic で落ちるテストの後のテストも記録されること、キャッシュの判定を通常の実行と kill matrix で正しく使い分けることも確かめる。
func TestKillMatrix(t *testing.T) {
	dir := copyModule(t, "../../testdata/killmatrix")
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	type matrix struct {
		Excluded   int        `json:"excluded"`
		NoKill     []string   `json:"testsDetectingNothing"`
		Duplicates [][]string `json:"duplicateTests"`
		Subsumed   []struct {
			Test string   `json:"test"`
			By   []string `json:"by"`
		} `json:"subsumedTests"`
	}
	gamutRun := func(args ...string) (map[string][]string, *matrix, string) {
		t.Helper()
		// 中身を変えずに更新時刻だけを変え、変更のない再実行の近道を外す。
		later := time.Now().Add(time.Minute)
		if err := os.Chtimes(filepath.Join(dir, "calc_test.go"), later, later); err != nil {
			t.Fatal(err)
		}
		reportPath := filepath.Join(t.TempDir(), "r.json")
		var stdout, stderr strings.Builder
		all := slices.Concat([]string{"-only", "ARITHMETIC_BASE", "-o", reportPath, "-cache", cachePath}, args, []string{dir})
		if code := realMain(all, &stdout, &stderr); code != 0 {
			t.Fatalf("%v: exit %d:\n%s", args, code, stderr.String())
		}
		data, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatal(err)
		}
		var rep struct {
			Gamut struct {
				Matrix *matrix `json:"killMatrix"`
			} `json:"gamut"`
		}
		if err := json.Unmarshal(data, &rep); err != nil {
			t.Fatal(err)
		}
		// kill matrix のときの killedBy は、検出したテストのすべてである。
		killers := map[string][]string{}
		for _, m := range readReport(t, reportPath) {
			killers[m.Original] = m.Killers
		}
		return killers, rep.Gamut.Matrix, stdout.String()
	}
	want := map[string][]string{"+": {"TestAdd", "TestAddAgain", "TestBoth", "TestPanicky"}, "-": {"TestBoth"}}

	// 通常の実行は最初の検出で止めるので、kill matrix を出さない。
	if _, m, _ := gamutRun(); m != nil {
		t.Errorf("a normal run reported a kill matrix: %+v", m)
	}
	// 通常の実行の KILLED は再利用せず、kill matrix のために実行し直す。2 回目は kill matrix の判定を再利用する。
	for _, run := range []string{"after a normal run", "reusing the matrix"} {
		killers, m, summary := gamutRun("-kill-matrix")
		if !maps.EqualFunc(killers, want, slices.Equal) {
			t.Errorf("%s: killers %v, want %v", run, killers, want)
		}
		if m == nil {
			t.Fatalf("%s: no kill matrix", run)
		}
		if !slices.Equal(m.NoKill, []string{"TestNothing"}) {
			t.Errorf("%s: tests detecting nothing %v, want [TestNothing]", run, m.NoKill)
		}
		if len(m.Duplicates) != 1 || !slices.Equal(m.Duplicates[0], []string{"TestAdd", "TestAddAgain", "TestPanicky"}) {
			t.Errorf("%s: duplicates %v", run, m.Duplicates)
		}
		var subsumed []string
		for _, s := range m.Subsumed {
			if !slices.Equal(s.By, []string{"TestBoth"}) {
				t.Errorf("%s: %s subsumed by %v, want [TestBoth]", run, s.Test, s.By)
			}
			subsumed = append(subsumed, s.Test)
		}
		if !slices.Equal(subsumed, []string{"TestAdd", "TestAddAgain", "TestPanicky"}) {
			t.Errorf("%s: subsumed %v", run, subsumed)
		}
		if m.Excluded != 0 {
			t.Errorf("%s: %d excluded, want 0", run, m.Excluded)
		}
		for _, w := range []string{"kill matrix", "TestNothing", "subsumed"} {
			if !strings.Contains(summary, w) {
				t.Errorf("%s: the summary lacks %q:\n%s", run, w, summary)
			}
		}
	}
}
