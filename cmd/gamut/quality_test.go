package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// qualityModule は、重要領域の関数 Fee に生き残りを 1 体（LIVED）持ち、ほかの変異体は検出されるモジュールを作る。
func qualityModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "go.mod", "module quality\n\ngo 1.27.4\n")
	write(t, dir, "fee.go", `package quality

// Fee は金額を計算する。
//
//gamut:critical
func Fee(a int) int { return a * 2 }
`)
	write(t, dir, "plain.go", "package quality\n\nfunc Sum(a, b int) int { return a + b }\n")
	write(t, dir, "quality_test.go", `package quality

import "testing"

func TestAll(t *testing.T) {
	if Fee(2) < 0 || Sum(1, 2) != 3 {
		t.Fatal("wrong")
	}
}
`)
	return dir
}

func exit(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var stderr strings.Builder
	code := realMain(append([]string{"-q", "-workers", "2"}, args...), io.Discard, &stderr)
	return code, stderr.String()
}

func TestQuality_CriticalSurvivorsFailUntilAccepted(t *testing.T) {
	dir := qualityModule(t)
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	code, stderr := exit(t, "-cache", cachePath, "-only", "ARITHMETIC_BASE", dir)
	if code != exitQuality || !strings.Contains(stderr, "1 survivors in critical regions") {
		t.Fatalf("exit %d, want %d for the critical survivor:\n%s", code, exitQuality, stderr)
	}
	// 変更のない再実行は前回の報告を返すが、品質の条件は同じく当てる。
	if code, stderr := exit(t, "-cache", cachePath, "-only", "ARITHMETIC_BASE", dir); code != exitQuality {
		t.Errorf("unchanged rerun: exit %d, want %d:\n%s", code, exitQuality, stderr)
	}
	// 品質の条件を満たさなくても、報告は書く。
	reportPath := filepath.Join(t.TempDir(), "report.json")
	if code, _ := exit(t, "-no-cache", "-only", "ARITHMETIC_BASE", "-o", reportPath, dir); code != exitQuality {
		t.Fatalf("exit %d, want %d", code, exitQuality)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var rep acceptReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	var id string
	for _, f := range rep.Files {
		for _, m := range f.Mutations {
			if m.Status == "LIVED" {
				id = m.ID
			}
		}
	}
	if id == "" {
		t.Fatal("no LIVED mutant")
	}
	if code := realMain([]string{"accept", "-reason", "callers read only the sign", "-dir", dir, id}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("accept exited %d", code)
	}
	if code, stderr := exit(t, "-cache", cachePath, "-only", "ARITHMETIC_BASE", dir); code != exitOK {
		t.Errorf("after accepting: exit %d, want 0:\n%s", code, stderr)
	}
}

func TestQuality_GlobsThresholdsAndFailOnSurvived(t *testing.T) {
	dir := qualityModule(t)
	write(t, dir, "fee.go", "package quality\n\nfunc Fee(a int) int { return a * 2 }\n")
	cases := []struct {
		name string
		args []string
		want int
		msg  string
	}{
		{"no conditions", nil, exitOK, ""},
		{"a critical glob", []string{"-critical", "fee.go"}, exitQuality, "1 survivors in critical regions"},
		{"a glob elsewhere", []string{"-critical", "plain.go"}, exitOK, ""},
		// 2 体のうち 1 体を検出するので、点数は 50%。
		{"a break threshold above the score", []string{"-threshold-break", "60"}, exitQuality, "below the break threshold 60%"},
		{"a break threshold at the score", []string{"-threshold-break", "50"}, exitOK, ""},
		{"fail on survived", []string{"-fail-on-survived"}, exitQuality, "1 survivors with -fail-on-survived"},
		{"thresholds out of order", []string{"-threshold-low", "90", "-threshold-high", "80"}, exitError, "must not exceed"},
		{"an unknown flag", []string{"-no-such-flag"}, exitError, "not defined"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stderr := exit(t, append(append([]string{"-no-cache", "-only", "ARITHMETIC_BASE"}, tc.args...), dir)...)
			if code != tc.want || !strings.Contains(stderr, tc.msg) {
				t.Errorf("exit %d, want %d with %q:\n%s", code, tc.want, tc.msg, stderr)
			}
		})
	}
	// 設定ファイルの項目も同じ意味を持つ。
	write(t, dir, "gamut.json", `{"critical": ["fee.go"], "thresholds": {"break": 60}, "fail_on_survived": true}`)
	code, stderr := exit(t, "-no-cache", "-only", "ARITHMETIC_BASE", dir)
	for _, msg := range []string{"critical regions", "break threshold", "-fail-on-survived"} {
		if code != exitQuality || !strings.Contains(stderr, msg) {
			t.Errorf("with gamut.json: exit %d, want %d mentioning %q:\n%s", code, exitQuality, msg, stderr)
		}
	}
}

func TestQuality_BaselineAndSelfCheckFailures(t *testing.T) {
	baseline := t.TempDir()
	write(t, baseline, "go.mod", "module base\n\ngo 1.27.4\n")
	write(t, baseline, "base.go", "package base\n\nfunc Sum(a, b int) int { return a + b }\n")
	write(t, baseline, "base_test.go", "package base\n\nimport \"testing\"\n\nfunc TestSum(t *testing.T) {\n\tif Sum(1, 2) != 4 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n")
	if code, stderr := exit(t, "-no-cache", baseline); code != exitBaseline {
		t.Errorf("failing baseline: exit %d, want %d:\n%s", code, exitBaseline, stderr)
	}
	// 対象のコードが panic を握りつぶすと、forced fail がテストに届かない。
	selfCheck := t.TempDir()
	write(t, selfCheck, "go.mod", "module swallow\n\ngo 1.27.4\n")
	write(t, selfCheck, "swallow.go", "package swallow\n\nfunc Sum(a, b int) (s int) {\n\tdefer func() { _ = recover() }()\n\treturn a + b\n}\n")
	write(t, selfCheck, "swallow_test.go", "package swallow\n\nimport \"testing\"\n\nfunc TestSum(t *testing.T) { _ = Sum(1, 2) }\n")
	if code, stderr := exit(t, "-no-cache", selfCheck); code != exitSelfCheck {
		t.Errorf("swallowed forced fail: exit %d, want %d:\n%s", code, exitSelfCheck, stderr)
	}
}
