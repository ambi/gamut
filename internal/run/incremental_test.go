package run_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

// judgement は、judge の 1 回の実行で観測したものである。
type judgement struct {
	results  map[string]*run.Result
	measured []string
	// groups は、実行した変異体の識別子から、テストのプロセスごとのテストの並びを引く。
	groups map[string][][]string
}

// judge は、gamut の main と同じ手順で dir の 1 パッケージを読み、鍵を付け、store（nil ならキャッシュなし）で判定する。
func judge(t *testing.T, dir string, store *cache.Store) judgement {
	t.Helper()
	pkg, err := loadOne(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	mutants := mutant.Enumerate(pkg, mutant.AllTypes)
	mutant.CheckViability(pkg, mutants)
	target := run.Target{Pkg: pkg, Mutants: mutants}
	if store != nil {
		sources, err := load.SourceDeps([]*load.Package{pkg})
		if err != nil {
			t.Fatal(err)
		}
		if target.Keys, err = cache.NewKeys(pkg, sources[pkg.ImportPath]); err != nil {
			t.Fatal(err)
		}
	}
	j := judgement{results: map[string]*run.Result{}, groups: map[string][][]string{}}
	opts := run.Options{Workers: 2, TimeoutFactor: 5, TimeoutMin: time.Second, Cache: store}
	opts = run.OnCoverage(opts, func(names []string) { j.measured = slices.Sorted(slices.Values(names)) })
	var mu sync.Mutex
	opts = run.OnExecute(opts, func(id string, groups [][]string) {
		mu.Lock()
		defer mu.Unlock()
		j.groups[id] = groups
	})
	results, err := run.All(context.Background(), []run.Target{target}, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results[0] {
		j.results[r.Mutant.ID] = r
	}
	return j
}

func copyDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

func edit(t *testing.T, dir, name, old, repl string) {
	t.Helper()
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain %q", name, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, repl, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestAll_RemeasuresCoverageOnlyForAffectedTests は、固定モジュールのコピーを書き換えながら判定し、
// 被覆の記録で実行するテストが、変わった宣言に入っていたテストと、変わったテストと、新しいテストだけであることと、
// キャッシュなしの判定と一致することを確かめる。
func TestAll_RemeasuresCoverageOnlyForAffectedTests(t *testing.T) {
	dir := copyDir(t, "../../testdata/reuse")
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"), "env")
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		name string
		edit func()
		want []string
	}{
		{"first run measures every test", nil, []string{"TestClamp", "TestScore", "TestTwice"}},
		{"unchanged rerun measures nothing", nil, nil},
		{"comments measure nothing", func() {
			edit(t, dir, "reuse.go", "\treturn v + v\n", "\t// 注釈。\n\treturn v + v\n")
		}, nil},
		{"changing a function measures the tests that entered it", func() {
			edit(t, dir, "reuse.go", "\treturn v + v\n", "\tw := v\n\treturn w + v\n")
		}, []string{"TestTwice"}},
		// Score は Clamp を呼ぶので、Clamp の変更は TestScore の到達も変えうる。
		{"changing a helper measures the tests that entered it or its callers", func() {
			edit(t, dir, "reuse.go", "\tif v > hi {", "\tif hi < v {")
		}, []string{"TestClamp", "TestScore"}},
		{"changing a test measures that test", func() {
			edit(t, dir, "reuse_test.go", "{{0, 0}, {60, 100}, {-5, 0}}", "{{0, 0}, {60, 100}, {-5, 0}, {1, 2}}")
		}, []string{"TestScore"}},
		{"a new test is measured", func() {
			edit(t, dir, "reuse_test.go", "func TestTwice(", "func TestTwiceZero(t *testing.T) {\n\tif Twice(0) != 0 {\n\t\tt.Error(\"Twice(0)\")\n\t}\n}\n\nfunc TestTwice(")
		}, []string{"TestTwiceZero"}},
		{"changing a package variable measures every test", func() {
			edit(t, dir, "reuse.go", "package reuse\n", "package reuse\n\nvar scale = 1 + 1\n")
		}, []string{"TestClamp", "TestScore", "TestTwice", "TestTwiceZero"}},
	}
	for _, step := range steps {
		if step.edit != nil {
			step.edit()
		}
		got := judge(t, dir, store)
		if !slices.Equal(got.measured, step.want) {
			t.Errorf("%s: measured %v, want %v", step.name, got.measured, step.want)
		}
		fresh := judge(t, dir, nil)
		for id, w := range fresh.results {
			g := got.results[id]
			if g == nil || g.Status != w.Status || g.Reason != w.Reason || g.Mode != w.Mode || !slices.Equal(g.Tests, w.Tests) {
				t.Errorf("%s: %s: with the cache %+v, without %+v", step.name, id, g, w)
			}
		}
		if t.Failed() {
			t.Fatalf("stopping after step %q", step.name)
		}
	}
}

// TestAll_RunsThePreviousKillerFirst は、前回 KILLED だった変異体を実行し直すとき、最初のプロセスが前回の検出テストだけを実行することを確かめる。
// TestCalls は宣言の順で先に走り、同じ変異体に感染するが検出しない。
func TestAll_RunsThePreviousKillerFirst(t *testing.T) {
	dir := copyDir(t, "../../testdata/ordering")
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"), "env")
	if err != nil {
		t.Fatal(err)
	}
	first := judge(t, dir, store)
	var target string
	for id, r := range first.results {
		if r.Mutant.Type == mutant.ArithmeticBase {
			target = id
			if r.Status != run.Killed || r.Killer != "TestDouble" {
				t.Fatalf("first run: %s %s by %q, want KILLED by TestDouble", id, r.Status, r.Killer)
			}
			if g := first.groups[id]; len(g) != 1 || !slices.Equal(g[0], []string{"TestCalls", "TestDouble"}) {
				t.Errorf("first run groups = %v, want one process with both tests", g)
			}
		}
	}
	// 検出したテストの本文を変えると、判定は再利用できず、実行し直す。
	edit(t, dir, "ordering_test.go", "\tif got := Double(3); got != 6 {", "\tgot := Double(3)\n\tif got != 6 {")
	second := judge(t, dir, store)
	r := second.results[target]
	if r.Cached || r.Status != run.Killed {
		t.Fatalf("second run: %s cached=%t, want executed and KILLED", r.Status, r.Cached)
	}
	// 1 体 1 プロセスでは前回の検出テストだけのプロセスを先に起動し、常駐するテストプロセスでは並びの先頭に置く。
	if g := second.groups[target]; len(g) == 0 || len(g[0]) == 0 || g[0][0] != "TestDouble" {
		t.Errorf("second run groups = %v, want the previous killer TestDouble first", g)
	}
}

func TestPhases_OrdersPreviousKillerSiteKillersThenTheRest(t *testing.T) {
	ms := time.Millisecond
	tests := map[string]time.Duration{"TestA": 3 * ms, "TestB": 1 * ms, "TestC": 2 * ms, "app.TestX": 1 * ms, "app.TestY": 1 * ms}
	cases := []struct {
		name  string
		prior []string
		want  [][]string
	}{
		{"no prior killer groups by binary, own first", nil, [][]string{{"TestA", "TestB", "TestC"}, {"app.TestX", "app.TestY"}}},
		{"previous killer alone first, then site killers, then the rest", []string{"TestC", "app.TestY", "TestA"},
			[][]string{{"TestC"}, {"TestA"}, {"app.TestY"}, {"TestB"}, {"app.TestX"}}},
		{"a prior killer that is gone is skipped", []string{"TestGone", "TestA"}, [][]string{{"TestA"}, {"TestB", "TestC"}, {"app.TestX", "app.TestY"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := run.Phases(tests, tc.prior)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if !slices.Equal(got[i], tc.want[i]) {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
