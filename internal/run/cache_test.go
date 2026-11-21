package run_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

// TestAll_RecordsVerdictsFinishedBeforeCancel checks that cancelling midway leaves in the cache only the verdicts finished by then.
// A run stopped after cancellation looks like a failure, so it must not be recorded as KILLED.
func TestAll_RecordsVerdictsFinishedBeforeCancel(t *testing.T) {
	pkg, err := loadOne("../../testdata/fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	mutants := mutant.Enumerate(pkg, classic)
	mutant.CheckViability(pkg, mutants)
	sources, err := load.SourceDeps([]*load.Package{pkg})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cache.NewKeys(pkg, sources[pkg.ImportPath])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cache.json")
	store, err := cache.Open(path, "env")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts := run.AfterJob(run.Options{Workers: 1, TimeoutFactor: 5, TimeoutMin: time.Second, Cache: store}, cancel)
	_, err = run.All(ctx, []run.Target{{Pkg: pkg, Mutants: mutants, Keys: keys}}, opts)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := cache.Open(path, "env")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Len() != 1 {
		t.Fatalf("cache holds %d verdicts, want exactly the 1 finished before cancel", reopened.Len())
	}

	// The one recorded mutant is reused, the rest run, and the verdicts match those without a cache.
	results, err := run.Run(context.Background(), pkg, mutants, run.Options{Workers: 2, TimeoutFactor: 5, TimeoutMin: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	opts = run.Options{Workers: 2, TimeoutFactor: 5, TimeoutMin: time.Second, Cache: reopened}
	resumed, err := run.All(context.Background(), []run.Target{{Pkg: pkg, Mutants: mutants, Keys: keys}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	reused := 0
	for i, r := range resumed[0] {
		if r.Cached {
			reused++
		}
		if r.Status != results[i].Status || r.Reason != results[i].Reason {
			t.Errorf("%s: resumed %s/%s, fresh %s/%s", r.Mutant.ID, r.Status, r.Reason, results[i].Status, results[i].Reason)
		}
	}
	if reused != 1 {
		t.Errorf("reused %d verdicts, want 1", reused)
	}
}
