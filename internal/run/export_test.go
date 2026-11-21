package run

import (
	"bytes"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ambi/gamut/internal/mutant"
)

// IgnoreSelection returns settings that run with a rewrite broken so the helper file does not read GAMUT_MUTANT.
// It serves only to reproduce the bug where mutations do not reach the tests, and to check that the self-check finds it.
func IgnoreSelection(o Options) Options {
	o.rewriteHelper = func(src []byte) []byte {
		return bytes.Replace(src, []byte(`var gamutActive = gamutEnvInt("GAMUT_MUTANT")`), []byte(`var gamutActive = 0`), 1)
	}
	return o
}

// AfterJob returns settings that call fn each time a mutant finishes running. Tests that cancel midway use it.
func AfterJob(o Options, fn func()) Options {
	o.afterJob = fn
	return o
}

// OnExecute returns settings that pass fn the list of tests per test process before a mutant runs.
func OnExecute(o Options, fn func(id string, groups [][]string)) Options {
	o.onExecute = func(m *mutant.Mutant, groups [][]string) { fn(m.ID, groups) }
	return o
}

// OnCoverage returns settings that pass fn the names of the tests run to record coverage.
func OnCoverage(o Options, fn func(names []string)) Options {
	o.onCoverage = fn
	return o
}

// Phases returns, from test names and durations, the lists per process with the previous detecting tests prior first.
// A test whose name contains "." is treated as a test of the reverse dependency whose import path is the part before it.
func Phases(tests map[string]time.Duration, prior []string) [][]string {
	own := &binary{pkg: "own"}
	bins := []*binary{own}
	byPkg := map[string]*binary{}
	var ts []*testCase
	for _, name := range slices.Sorted(maps.Keys(tests)) {
		b := own
		if pkg, local, ok := strings.Cut(name, "."); ok {
			if byPkg[pkg] == nil {
				byPkg[pkg] = &binary{pkg: pkg, prefix: pkg + "."}
				bins = append(bins, byPkg[pkg])
			}
			b = byPkg[pkg]
			name = local
		}
		t := newTestCase(b, name, "")
		t.duration = tests[b.prefix+name]
		ts = append(ts, t)
	}
	var out [][]string
	for _, g := range phases(ts, prior, bins) {
		out = append(out, testNames(g))
	}
	return out
}

// NoBatch returns settings that record coverage only with one process per test. It serves to compare with batched recording.
func NoBatch(o Options) Options {
	o.noBatch = true
	return o
}
