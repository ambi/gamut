package main

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// mutation is one mutant of the report in a form easy for tests to compare (made by readReport).
type mutation struct {
	ID     string
	Type   string
	Status string
	// Reason is statusReason, and Original the text before replacement.
	Reason   string
	Original string
	Mode     string
	Cached   bool
	// KilledBy is the first detecting test, Killers all detecting tests, and PreviouslyKilledBy the previous detecting test that is gone.
	KilledBy           string
	Killers            []string
	PreviouslyKilledBy string
	// Tests are the tests that reached the mutation site.
	Tests    []string
	Critical bool
	// Accepted is the reason for acceptance, and AcceptedStatus the judged status of an accepted mutant.
	Accepted       string
	AcceptedStatus string
}

// scope extracts the enclosing declaration's name from an identifier.
func (m mutation) scope() string {
	parts := strings.Split(m.ID, "::")
	return parts[2]
}

// executed reports whether the mutant was run for judging. Mutants not reached and not infected are never run.
func (m mutation) executed() bool {
	needsRun := m.Status == "Killed" || m.Status == "Timeout" || m.Status == "Survived" && m.Reason == "not detected"
	return needsRun && !m.Cached
}

// TestCache_ReusesOnlyVerdictsWhoseKeysAreUnchanged runs gamut while editing a copy of a fixed module, and checks
// that only mutants the change affects run again, and that the verdicts match those without a cache.
func TestCache_ReusesOnlyVerdictsWhoseKeysAreUnchanged(t *testing.T) {
	dir := copyModule(t, "../../testdata/reuse")
	cachePath := filepath.Join(t.TempDir(), "cache.json")

	steps := []struct {
		name string
		edit func(t *testing.T)
		// check receives the enclosing declarations of the mutants run in the run with a cache.
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
				replace(t, dir, "reuse.go", "package reuse\n", "package reuse\n\n// A note that only shifts lines.\n\n")
				replace(t, dir, "reuse.go", "\treturn v + v\n", "\t// A note in the body.\n\treturn v +\n\t\tv\n")
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
				replace(t, dir, "go.mod", "go 1.27.4\n", "go 1.27.4\n\n// A change to the environment.\n")
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
		// Verdicts match with and without the cache.
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

// wantExecuted checks that the enclosing declarations of the run mutants are exactly scopes.
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

// TestCache_ReportsUnchangedInputsWithoutLoading checks that a rerun with unchanged inputs returns the same verdicts without loading or building,
// and that adding a file to a package directory, changing testdata, or changing flags skips the shortcut.
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
		// Record the latest state in the shortcut.
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
