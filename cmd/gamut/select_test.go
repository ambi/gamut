package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// gamut は、gamut を args で実行し、報告の変異体、標準エラー出力、終了コードを返す。報告は終了コードが 0 のときだけ読む。
func gamut(t *testing.T, args ...string) ([]mutation, string, int) {
	t.Helper()
	reportPath := filepath.Join(t.TempDir(), "report.json")
	args = append([]string{"-workers", "2", "-o", reportPath}, args...)
	var stderr strings.Builder
	code := realMain(args, io.Discard, &stderr)
	if code != 0 {
		return nil, stderr.String(), code
	}
	return readReport(t, reportPath), stderr.String(), 0
}

func mustGamut(t *testing.T, args ...string) ([]mutation, string) {
	t.Helper()
	ms, stderr, code := gamut(t, args...)
	if code != 0 {
		t.Fatalf("gamut %v exited %d:\n%s", args, code, stderr)
	}
	return ms, stderr
}

// gitRepo は、dir を 1 回コミットした Git リポジトリにする。
func gitRepo(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "init", "-q", "-b", "main")
	commit(t, dir)
}

func commit(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "change")
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// byScope は、変異体を囲む宣言ごとに、状態を並べる。
func byScope(ms []mutation) map[string][]string {
	out := map[string][]string{}
	for _, m := range ms {
		out[m.scope()] = append(out[m.scope()], m.Status)
	}
	return out
}

func allPending(statuses []string) bool {
	return !slices.ContainsFunc(statuses, func(s string) bool { return s != "Pending" })
}

func nonePending(statuses []string) bool {
	return len(statuses) > 0 && !slices.Contains(statuses, "Pending")
}

func TestSince_JudgesOnlyMutantsInTheDiff(t *testing.T) {
	dir := copyModule(t, "../../testdata/reuse")
	gitRepo(t, dir)
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	before, _ := mustGamut(t, "-q", "-cache", cachePath, dir)

	replace(t, dir, "reuse.go", "\treturn v + v\n", "\tw := v\n\treturn w + v\n")
	ms, _ := mustGamut(t, "-q", "-cache", cachePath, "-since", "HEAD", dir)
	if len(ms) != len(before) {
		t.Fatalf("-since reported %d mutants, want all %d with the rest PENDING", len(ms), len(before))
	}
	scopes := byScope(ms)
	if !nonePending(scopes["Twice"]) || !allPending(scopes["Clamp"]) || !allPending(scopes["Score"]) {
		t.Errorf("statuses = %v, want only Twice judged", scopes)
	}

	t.Run("decl scope catches a signature change", func(t *testing.T) {
		commit(t, dir)
		replace(t, dir, "reuse.go", "func Score(v int) int {", "func Score(v int) (score int) {")
		node, _ := mustGamut(t, "-q", "-cache", cachePath, "-since", "HEAD", dir)
		if s := byScope(node)["Score"]; !allPending(s) {
			t.Errorf("node scope: Score = %v, want PENDING because no operator changed", s)
		}
		decl, _ := mustGamut(t, "-q", "-cache", cachePath, "-since", "HEAD", "-scope", "decl", dir)
		if s := byScope(decl)["Score"]; !nonePending(s) {
			t.Errorf("decl scope: Score = %v, want judged", s)
		}
		commit(t, dir)
	})

	t.Run("survivors reached by a changed test are rechecked on request", func(t *testing.T) {
		replace(t, dir, "reuse_test.go", "{{0, 0}, {60, 100}, {-5, 0}}", "{{0, 0}, {60, 100}, {-5, 0}, {1, 2}}")
		without, _ := mustGamut(t, "-q", "-cache", cachePath, "-since", "HEAD", dir)
		if s := byScope(without)["Score"]; !allPending(s) {
			t.Errorf("without -include-test-changes: Score = %v, want PENDING", s)
		}
		with, _ := mustGamut(t, "-q", "-cache", cachePath, "-since", "HEAD", "-include-test-changes", dir)
		for _, m := range with {
			// 前回 KILLED でない Score の変異体（生き残り）は、変えたテスト TestScore が到達するので再検査する。
			if m.scope() == "Score" && m.Status == "Pending" && !slices.ContainsFunc(before, func(b mutation) bool { return b.ID == m.ID && b.Status == "Killed" }) {
				t.Errorf("with -include-test-changes: %s stays PENDING", m.ID)
			}
		}
		if s := byScope(with)["Twice"]; !allPending(s) {
			t.Errorf("with -include-test-changes: Twice = %v, want PENDING because TestScore does not reach it", s)
		}
		commit(t, dir)
	})

	t.Run("mutants whose killer was deleted are rechecked outside the diff", func(t *testing.T) {
		// 前回の判定をキャッシュに揃える。
		full, _ := mustGamut(t, "-q", "-cache", cachePath, dir)
		killedByClamp := map[string]bool{}
		for _, m := range full {
			if m.Status == "Killed" && strings.HasPrefix(m.KilledBy, "TestClamp") {
				killedByClamp[m.ID] = true
			}
		}
		if len(killedByClamp) == 0 {
			t.Fatal("fixture: no mutant is killed by TestClamp")
		}
		replace(t, dir, "reuse_test.go", "func TestClamp(", "func removedClamp(")
		ms, _ := mustGamut(t, "-q", "-cache", cachePath, "-since", "HEAD", dir)
		for _, m := range ms {
			switch {
			case killedByClamp[m.ID] && m.Status == "Pending":
				t.Errorf("%s: killed by the deleted TestClamp but not rechecked", m.ID)
			case !killedByClamp[m.ID] && m.Status != "Pending":
				t.Errorf("%s: %s, want PENDING outside the diff", m.ID, m.Status)
			case killedByClamp[m.ID] && m.Status == "Killed" && m.PreviouslyKilledBy != "":
				t.Errorf("%s: killed again but reported as previously killed by %q", m.ID, m.PreviouslyKilledBy)
			}
		}

		// 残った検出テスト TestScore も消すと、Clamp と Score の変異体はどのテストにも検出されなくなる。
		commit(t, dir)
		replace(t, dir, "reuse_test.go", "func TestScore(", "func removedScore(")
		ms, _ = mustGamut(t, "-q", "-cache", cachePath, "-since", "HEAD", dir)
		regressed := 0
		for _, m := range ms {
			if m.PreviouslyKilledBy == "" {
				continue
			}
			regressed++
			if m.PreviouslyKilledBy != "TestScore" || m.Status != "NoCoverage" {
				t.Errorf("%s: %s previously killed by %q, want NOT COVERED previously killed by TestScore", m.ID, m.Status, m.PreviouslyKilledBy)
			}
		}
		if regressed == 0 {
			t.Errorf("no mutant is reported as no longer detected: %+v", ms)
		}
	})
}

func TestSince_FailsWithoutAResolvableRef(t *testing.T) {
	dir := copyModule(t, "../../testdata/reuse")
	gitRepo(t, dir)
	if _, stderr, code := gamut(t, "-q", "-no-cache", "-since", "no-such-ref", dir); code != 1 {
		t.Errorf("exit code = %d, want 1\n%s", code, stderr)
	}
}

func TestMutant_RerunsOnlyTheNamedMutant(t *testing.T) {
	dir := copyModule(t, "../../testdata/rerun")
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	all, _ := mustGamut(t, "-q", "-cache", cachePath, dir)
	var survivor mutation
	for _, m := range all {
		if m.scope() == "IsAdult" && m.Type == "CONDITIONALS_BOUNDARY" {
			survivor = m
		}
	}
	if survivor.Status != "Survived" {
		t.Fatalf("fixture: IsAdult boundary mutant is %q, want LIVED", survivor.Status)
	}
	prefix := survivor.ID[:len(survivor.ID)-4]

	replace(t, dir, "rerun_test.go", "if !IsAdult(30) || IsAdult(5) {", "if !IsAdult(30) || IsAdult(5) || !IsAdult(18) {")
	ms, stderr := mustGamut(t, "-cache", cachePath, "-mutant", prefix, dir)
	if len(ms) != 1 || ms[0].ID != survivor.ID {
		t.Fatalf("reported %v, want only %s", ms, survivor.ID)
	}
	if ms[0].Status != "Killed" || ms[0].Cached {
		t.Errorf("after adding a test: %s cached=%t, want executed and KILLED", ms[0].Status, ms[0].Cached)
	}
	// 被覆を測り直すのは、指定した変異体の変異箇所に到達していた（そして変わった）TestIsAdult だけである。
	if !strings.Contains(stderr, "coverage: 1 tests measured (1 one by one), 1 restored") {
		t.Errorf("want only TestIsAdult measured, log:\n%s", stderr)
	}
	// 判定はキャッシュに書き戻され、次の実行で再利用される。
	again, _ := mustGamut(t, "-q", "-cache", cachePath, dir)
	for _, m := range again {
		if m.ID == survivor.ID && (m.Status != "Killed" || !m.Cached) {
			t.Errorf("next run: %s cached=%t, want the written-back KILLED reused", m.Status, m.Cached)
		}
	}

	t.Run("an ambiguous prefix lists the candidates", func(t *testing.T) {
		_, stderr, code := gamut(t, "-q", "-no-cache", "-mutant", "rerun.go::", dir)
		if code != 1 || !strings.Contains(stderr, survivor.ID) {
			t.Errorf("exit code = %d, want 1 with the candidates listed:\n%s", code, stderr)
		}
	})
	t.Run("-mutant and -since cannot be combined", func(t *testing.T) {
		if _, _, code := gamut(t, "-q", "-mutant", prefix, "-since", "HEAD", dir); code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
	})
}

func TestIntegration_DetectsWithReverseDependencyTests(t *testing.T) {
	dir := copyTree(t, "../../testdata/integration")
	lib := filepath.Join(dir, "lib")
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	status := func(ms []mutation) map[string]string {
		out := map[string]string{}
		for _, m := range ms {
			out[m.scope()+" "+m.Type] = m.Status
		}
		return out
	}
	alone, _ := mustGamut(t, "-q", "-no-cache", lib)
	if got := status(alone); got["Add ARITHMETIC_BASE"] != "Survived" || got["Triple ARITHMETIC_BASE"] != "NoCoverage" {
		t.Errorf("without -integration: %v, want Add LIVED and Triple NOT COVERED", got)
	}
	together, stderr := mustGamut(t, "-cache", cachePath, "-integration", lib)
	if got := status(together); got["Add ARITHMETIC_BASE"] != "Killed" || got["Triple ARITHMETIC_BASE"] != "Killed" {
		t.Errorf("with -integration: %v, want both KILLED", got)
	}
	for _, m := range together {
		if m.Status == "Killed" && !strings.HasPrefix(m.KilledBy, "integration/app.Test") {
			t.Errorf("%s killed by %q, want a test of integration/app", m.ID, m.KilledBy)
		}
	}
	// unrelated の 2 件は、まとめた 1 回の実行で到達がないと分かるので、1 件ずつは測らない。
	if !strings.Contains(stderr, "coverage: 5 tests measured (3 one by one)") {
		t.Errorf("want unrelated's tests measured in one probe, log:\n%s", stderr)
	}
	if !strings.Contains(stderr, "integration: integration/lib: no test of integration/unrelated reaches a mutated site") {
		t.Errorf("want unrelated reported as unreached, log:\n%s", stderr)
	}
	// 変更のない次の実行では、被覆の記録を戻し、判定を再利用するので、逆依存パッケージのテストバイナリをビルドしない。
	again, stderr := mustGamut(t, "-cache", cachePath, "-integration", lib)
	if strings.Contains(stderr, "build: integration/") && strings.Contains(stderr, "rewritten") {
		t.Errorf("want no reverse dependency built on an unchanged rerun, log:\n%s", stderr)
	}
	if got := status(again); got["Add ARITHMETIC_BASE"] != "Killed" || got["Triple ARITHMETIC_BASE"] != "Killed" {
		t.Errorf("rerun with -integration: %v, want both KILLED", got)
	}
	// app の検出テストを変えると、app のテストバイナリだけをビルドし直し、どのテストも到達しない unrelated はビルドしない。
	replace(t, filepath.Join(dir, "app"), "app_test.go", "\tif got := Sum3(1, 2, 3); got != 6 {", "\tgot := Sum3(1, 2, 3)\n\tif got != 6 {")
	third, stderr := mustGamut(t, "-cache", cachePath, "-integration", lib)
	if !strings.Contains(stderr, "build: integration/app with") || builds(stderr, "integration/unrelated") {
		t.Errorf("want only app rebuilt, log:\n%s", stderr)
	}
	if got := status(third); got["Add ARITHMETIC_BASE"] != "Killed" {
		t.Errorf("after changing app's test: %v, want Add KILLED", got)
	}
}

// builds は、ログの build の行のどれかが pkg をビルドしたかを返す。
func builds(log, pkg string) bool {
	for line := range strings.Lines(log) {
		if strings.HasPrefix(line, "build: ") && slices.Contains(strings.Fields(line), pkg) {
			return true
		}
	}
	return false
}

// copyTree は、src のディレクトリの木を一時ディレクトリへ写す。
func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	return dst
}
