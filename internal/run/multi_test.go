package run_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

func TestLoad_PatternListsEveryPackageWithSources(t *testing.T) {
	pkgs, err := load.Load([]string{"../../testdata/multi/..."}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, p := range pkgs {
		paths = append(paths, p.ImportPath)
		if p.Fset != pkgs[0].Fset {
			t.Errorf("%s: packages loaded together must share one FileSet", p.ImportPath)
		}
	}
	// d はテストファイルしか持たないので対象にならない。
	if got := strings.Join(paths, " "); got != "multi/a multi/b multi/c" {
		t.Fatalf("packages = %s, want multi/a multi/b multi/c", got)
	}
	if !pkgs[0].HasTests || !pkgs[1].HasTests || pkgs[2].HasTests {
		t.Errorf("HasTests = %t %t %t, want true true false", pkgs[0].HasTests, pkgs[1].HasTests, pkgs[2].HasTests)
	}
}

func TestAll_JudgesEveryPackageOfPatternLikeSeparateRuns(t *testing.T) {
	opts := run.Options{Workers: 2, TimeoutFactor: 5, TimeoutMin: time.Second}
	pkgs, err := load.Load([]string{"../../testdata/multi/..."}, nil)
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]run.Target, len(pkgs))
	for i, pkg := range pkgs {
		mutants := mutant.Enumerate(pkg, mutant.AllTypes)
		mutant.CheckViability(pkg, mutants)
		targets[i] = run.Target{Pkg: pkg, Mutants: mutants}
	}
	var log bytes.Buffer
	withLog := opts
	withLog.Log = &log
	perPackage, err := run.All(context.Background(), targets, withLog)
	if err != nil {
		t.Fatal(err)
	}
	together := map[string]verdict{}
	for _, results := range perPackage {
		for _, r := range results {
			together[r.Mutant.ID] = verdict{r.Status, r.Reason, r.Mode}
		}
	}

	want := map[string]run.Status{
		"a/a.go Max CONDITIONALS_BOUNDARY": run.Lived,
		"a/a.go Max CONDITIONALS_NEGATION": run.Killed,
		"b/b.go Scale ARITHMETIC_BASE":     run.Killed,
		"b/b.go Neg INVERT_NEGATIVES":      run.Killed,
		// テストファイルがないパッケージは、ビルドせずに到達なしとする。
		"c/c.go Half ARITHMETIC_BASE": run.NotCovered,
	}
	got := map[string]run.Status{}
	for _, results := range perPackage {
		for _, r := range results {
			got[r.Mutant.File.Rel+" "+r.Mutant.Scope+" "+string(r.Mutant.Type)] = r.Status
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d mutants, want %d: %v", len(got), len(want), got)
	}
	for key, w := range want {
		if got[key] != w {
			t.Errorf("%s: status %q, want %q", key, got[key], w)
		}
	}
	if strings.Contains(log.String(), "build: multi/c") {
		t.Errorf("a package without test files must not be built, log:\n%s", log.String())
	}

	// パッケージごとに分けて実行した判定と一致する。
	separate := map[string]verdict{}
	for _, dir := range []string{"../../testdata/multi/a", "../../testdata/multi/b", "../../testdata/multi/c"} {
		results, err := loadAndRun(t, dir, nil, opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			separate[r.Mutant.ID] = verdict{r.Status, r.Reason, r.Mode}
		}
	}
	if len(separate) != len(together) {
		t.Errorf("separate runs judged %d mutants, the combined run %d", len(separate), len(together))
	}
	for id, s := range separate {
		if together[id] != s {
			t.Errorf("%s: combined %+v, separate %+v", id, together[id], s)
		}
	}
}
