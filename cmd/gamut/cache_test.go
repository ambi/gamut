package main

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// mutation は、報告の 1 体の変異体を、テストが比べやすい形にしたものである（readReport が作る）。
type mutation struct {
	ID     string
	Type   string
	Status string
	// Reason は statusReason、Original は置き換える前の字句である。
	Reason   string
	Original string
	Mode     string
	Cached   bool
	// KilledBy は最初の検出テスト、Killers は検出したテストのすべて、PreviouslyKilledBy は消えた前回の検出テストである。
	KilledBy           string
	Killers            []string
	PreviouslyKilledBy string
	// Tests は、変異箇所に到達したテストである。
	Tests    []string
	Critical bool
	// Accepted は受理の理由、AcceptedStatus は受理した変異体の判定した状態である。
	Accepted       string
	AcceptedStatus string
}

// scope は識別子から囲む宣言の名前を取り出す。
func (m mutation) scope() string {
	parts := strings.Split(m.ID, "::")
	return parts[2]
}

// executed は、判定のために変異体を実行したかを返す。到達しない変異体と感染しない変異体はもともと実行しない。
func (m mutation) executed() bool {
	needsRun := m.Status == "Killed" || m.Status == "Timeout" || m.Status == "Survived" && m.Reason == "not detected"
	return needsRun && !m.Cached
}

// TestCache_ReusesOnlyVerdictsWhoseKeysAreUnchanged は、固定モジュールのコピーを書き換えながら gamut を実行し、
// 実行し直す変異体が変更の影響を受けるものだけであることと、キャッシュなしの判定と一致することを確かめる。
func TestCache_ReusesOnlyVerdictsWhoseKeysAreUnchanged(t *testing.T) {
	dir := copyModule(t, "../../testdata/reuse")
	cachePath := filepath.Join(t.TempDir(), "cache.json")

	steps := []struct {
		name string
		edit func(t *testing.T)
		// check は、キャッシュありの実行で実行した変異体の囲む宣言を受け取る。
		check func(t *testing.T, executed map[string]int, all []mutation)
	}{
		{
			name: "first run executes every mutant",
			check: func(t *testing.T, executed map[string]int, all []mutation) {
				for _, m := range all {
					if m.Cached {
						t.Errorf("%s: reused on the first run", m.ID)
					}
				}
				if executed["Clamp"] == 0 || executed["Score"] == 0 || executed["Twice"] == 0 {
					t.Errorf("executed = %v, want mutants of Clamp, Score, and Twice", executed)
				}
			},
		},
		{
			name:  "unchanged rerun executes nothing",
			check: wantExecuted(),
		},
		{
			name: "comments and shifted lines execute nothing",
			edit: func(t *testing.T) {
				replace(t, dir, "reuse.go", "package reuse\n", "package reuse\n\n// 行をずらすだけの注釈。\n\n")
				replace(t, dir, "reuse.go", "\treturn v + v\n", "\t// 本文の注釈。\n\treturn v +\n\t\tv\n")
			},
			check: wantExecuted(),
		},
		{
			name: "changing a function executes only its mutants",
			edit: func(t *testing.T) {
				replace(t, dir, "reuse.go", "\treturn v +\n\t\tv\n", "\tw := v\n\treturn w + v\n")
			},
			check: wantExecuted("Twice"),
		},
		{
			name: "changing a helper executes the survivors of its callers",
			edit: func(t *testing.T) {
				replace(t, dir, "reuse.go", "\tif v > hi {", "\tif hi < v {")
			},
			check: func(t *testing.T, executed map[string]int, all []mutation) {
				wantExecuted("Clamp", "Score")(t, executed, all)
				for _, m := range all {
					if m.scope() == "Score" && m.Status == "Survived" && m.Cached {
						t.Errorf("%s: the survivor of a caller of the changed helper was reused", m.ID)
					}
				}
			},
		},
		{
			name: "changing a test executes the mutants it reaches",
			edit: func(t *testing.T) {
				replace(t, dir, "reuse_test.go", "{{0, 0}, {60, 100}, {-5, 0}}", "{{0, 0}, {60, 100}, {-5, 0}, {1, 2}}")
			},
			check: wantExecuted("Clamp", "Score"),
		},
		{
			name: "changing go.mod executes every mutant",
			edit: func(t *testing.T) {
				replace(t, dir, "go.mod", "go 1.27.4\n", "go 1.27.4\n\n// 環境の変更。\n")
			},
			check: func(t *testing.T, _ map[string]int, all []mutation) {
				for _, m := range all {
					if m.Cached {
						t.Errorf("%s: reused after go.mod changed", m.ID)
					}
				}
			},
		},
	}
	for _, step := range steps {
		if step.edit != nil {
			step.edit(t)
		}
		cached := runGamut(t, dir, "-cache", cachePath)
		fresh := runGamut(t, dir, "-no-cache")
		executed := map[string]int{}
		for _, m := range cached {
			if m.executed() {
				executed[m.scope()]++
			}
		}
		t.Logf("%s: executed %v", step.name, executed)
		step.check(t, executed, cached)
		// キャッシュの有無で判定が一致する。
		want := map[string]mutation{}
		for _, m := range fresh {
			want[m.ID] = m
		}
		if len(cached) != len(fresh) {
			t.Errorf("%s: %d mutants with the cache, %d without", step.name, len(cached), len(fresh))
		}
		for _, m := range cached {
			w := want[m.ID]
			if m.Status != w.Status || m.Reason != w.Reason || m.Mode != w.Mode {
				t.Errorf("%s: %s: with the cache %s/%s/%s, without %s/%s/%s",
					step.name, m.ID, m.Status, m.Reason, m.Mode, w.Status, w.Reason, w.Mode)
			}
		}
		if t.Failed() {
			t.Fatalf("stopping after step %q", step.name)
		}
	}
}

// wantExecuted は、実行した変異体の囲む宣言が scopes とちょうど一致することを確かめる。
func wantExecuted(scopes ...string) func(*testing.T, map[string]int, []mutation) {
	return func(t *testing.T, executed map[string]int, _ []mutation) {
		t.Helper()
		var got []string
		for s := range executed {
			got = append(got, s)
		}
		slices.Sort(got)
		want := slices.Sorted(slices.Values(scopes))
		if !slices.Equal(got, want) {
			t.Errorf("executed mutants in %v, want exactly %v", got, want)
		}
	}
}

func runGamut(t *testing.T, dir string, flags ...string) []mutation {
	t.Helper()
	reportPath := filepath.Join(t.TempDir(), "report.json")
	args := append(slices.Clone(flags), "-q", "-workers", "2", "-o", reportPath, dir)
	var stderr strings.Builder
	if code := realMain(args, io.Discard, &stderr); code != 0 {
		t.Fatalf("gamut %v exited %d:\n%s", args, code, stderr.String())
	}
	return readReport(t, reportPath)
}

func copyModule(t *testing.T, src string) string {
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

func replace(t *testing.T, dir, name, old, repl string) {
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

// TestCache_ReportsUnchangedInputsWithoutLoading は、入力が前回と同じ再実行が、読み込みもビルドもせずに同じ判定を返し、
// パッケージのディレクトリへのファイルの追加、testdata の変更、フラグの変更では近道を使わないことを確かめる。
func TestCache_ReportsUnchangedInputsWithoutLoading(t *testing.T) {
	dir := copyModule(t, "../../testdata/reuse")
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	first, _ := mustGamut(t, "-cache", cachePath, dir)
	again, stderr := mustGamut(t, "-cache", cachePath, dir)
	if !strings.Contains(stderr, "no input changed") || strings.Contains(stderr, "load:") {
		t.Fatalf("want the unchanged rerun to skip loading, log:\n%s", stderr)
	}
	if len(again) != len(first) {
		t.Fatalf("replayed %d mutants, want %d", len(again), len(first))
	}
	for i, m := range again {
		f := first[i]
		if m.ID != f.ID || m.Status != f.Status || m.Reason != f.Reason {
			t.Errorf("%s: replayed %s/%s, first %s/%s", m.ID, m.Status, m.Reason, f.Status, f.Reason)
		}
		if m.executed() || (f.executed() && !m.Cached) {
			t.Errorf("%s: a replayed verdict that needs a run must be marked cached", m.ID)
		}
	}
	steps := []struct {
		name string
		edit func()
		args []string
	}{
		{"a new file in the package", func() {
			write(t, dir, "extra.go", "package reuse\n\nfunc Extra(v int) int { return v - 1 }\n")
		}, nil},
		{"a new testdata file", func() {
			write(t, filepath.Join(dir, "testdata"), "in.txt", "x\n")
		}, nil},
		{"another flag", nil, []string{"-only", "ARITHMETIC_BASE"}},
	}
	for _, step := range steps {
		// 直前の状態を近道に記録する。
		mustGamut(t, append(slices.Clone(step.args), "-cache", cachePath, dir)...)
		if step.edit != nil {
			step.edit()
		}
		_, stderr := mustGamut(t, append(slices.Clone(step.args), "-cache", cachePath, dir)...)
		if step.edit != nil && strings.Contains(stderr, "no input changed") {
			t.Errorf("%s: the rerun used the shortcut, log:\n%s", step.name, stderr)
		}
	}
}

func write(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}
