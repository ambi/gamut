package run

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/mutant"
)

var failLine = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)

// priorKillers returns, in the order to try, the names of the tests tried first when running m.
// The test that detected m last time comes first, followed by tests that detected other mutants at the same site.
// The cache is written after every run, so detections earlier in this run are included.
func (u *unit) priorKillers(m *mutant.Mutant, store *cache.Store) []string {
	if store == nil {
		return nil
	}
	var out []string
	add := func(id string) {
		if e := store.Get(id); e != nil && Status(e.Status) == Killed && e.Killer != "" && !slices.Contains(out, e.Killer) {
			out = append(out, e.Killer)
		}
	}
	add(m.ID)
	if s := u.sch.SiteOf[m]; s != nil {
		for _, other := range s.Mutants {
			if other != m {
				add(other.ID)
			}
		}
	}
	return out
}

// phases splits tests into groups, one per test process, and returns them in the order to run.
// The first holds only the previous detecting test (prior[0]), the second the rest of prior, and the last the remaining tests.
// Within one process tests run in declaration order, so the order in -test.run cannot set it; hence the separate processes.
// Each group holds tests of only one test binary, with the package's own test binary first.
func phases(tests []*testCase, prior []string, bins []*binary) [][]*testCase {
	byName := map[string]*testCase{}
	for _, t := range tests {
		byName[t.name] = t
	}
	used := map[*testCase]bool{}
	var out [][]*testCase
	group := func(ts []*testCase) {
		for _, b := range bins {
			var g []*testCase
			for _, t := range ts {
				if t.bin == b && !used[t] {
					g = append(g, t)
				}
			}
			for _, t := range g {
				used[t] = true
			}
			if len(g) > 0 {
				out = append(out, g)
			}
		}
	}
	var first, rest []*testCase
	for i, name := range prior {
		if t := byName[name]; t != nil {
			if i == 0 {
				first = append(first, t)
			} else {
				rest = append(rest, t)
			}
		}
	}
	group(first)
	group(rest)
	group(tests)
	return out
}

// execute enables the mutant m and runs tests, the previous detecting test first in its own process, stopping at the first failure.
// binPath returns the file to run per test binary. When it returns an error, the mutant is not viable.
func (u *unit) execute(ctx context.Context, m *mutant.Mutant, tests []*testCase, opts Options, binPath func(*binary) (string, error)) *Result {
	if opts.KillMatrix {
		return u.executeAll(ctx, m, tests, opts, binPath)
	}
	groups := phases(tests, u.priorKillers(m, opts.Cache), u.bins)
	if opts.onExecute != nil {
		names := make([][]string, len(groups))
		for i, g := range groups {
			names[i] = testNames(g)
		}
		opts.onExecute(m, names)
	}
	index := m.Index
	start := time.Now()
	r := &Result{Mutant: m, Status: Lived, Reason: ReasonNotDetected}
	for _, g := range groups {
		b := g[0].bin
		path, err := binPath(b)
		if err != nil {
			return &Result{Mutant: m, Status: NotViable, Elapsed: time.Since(start)}
		}
		if path != b.path {
			// A standalone binary has the mutation written in directly, so no selection number is passed.
			index = 0
		}
		quoted := make([]string, len(g))
		for i, t := range g {
			quoted[i] = regexp.QuoteMeta(t.local)
		}
		limit := time.Duration(float64(total(g))*opts.TimeoutFactor) + opts.TimeoutMin
		runCtx, cancel := context.WithTimeout(ctx, limit)
		cmd := testCommand(runCtx, b.dir, path, opts.TestFlags, []string{"GAMUT_MUTANT=" + strconv.Itoa(index)},
			"-test.run", "^(?:"+strings.Join(quoted, "|")+")$", "-test.count=1", "-test.failfast", "-test.paniconexit0")
		out, err := cmd.CombinedOutput()
		timedOut := runCtx.Err() != nil && ctx.Err() == nil
		cancel()
		switch {
		case timedOut:
			r.Status, r.Reason = TimedOut, ""
		case err != nil:
			r.Status, r.Reason = Killed, ""
			if f := failLine.FindSubmatch(out); f != nil {
				r.KilledBy = b.prefix + string(f[1])
				top, _, _ := strings.Cut(string(f[1]), "/")
				r.Killer = b.prefix + top
			} else if len(g) == 1 {
				// Even without a failure line (such as with os.Exit), when only one test ran, that test detected it.
				r.Killer = g[0].name
			}
		default:
			continue
		}
		break
	}
	r.Elapsed = time.Since(start)
	return r
}

// verboseLine matches the lines of -test.v output that start a top-level test (=== RUN) and give its result (--- PASS/FAIL/SKIP).
// Subtest result lines are indented and subtest start lines contain / in the name, so neither matches.
var verboseLine = regexp.MustCompile(`(?m)^(?:=== RUN\s+([^\s/]+)$|--- (PASS|FAIL|SKIP): ([^\s/]+) )`)

// executeAll, for the kill matrix, enables the mutant m, runs every test in tests, and records every failing top-level test.
// It does not stop at the first failure. When the process dies from a panic or os.Exit, the running test without a result counts as a detector,
// and the tests not yet started run again in a new process. On a timeout it reports TIMED OUT and records no detecting tests.
func (u *unit) executeAll(ctx context.Context, m *mutant.Mutant, tests []*testCase, opts Options, binPath func(*binary) (string, error)) *Result {
	start := time.Now()
	r := &Result{Mutant: m, Status: Lived, Reason: ReasonNotDetected, Matrix: true}
	if opts.onExecute != nil {
		opts.onExecute(m, [][]string{testNames(tests)})
	}
	killers := map[string]bool{}
	for _, b := range u.bins {
		var remaining []*testCase
		for _, t := range tests {
			if t.bin == b {
				remaining = append(remaining, t)
			}
		}
		if len(remaining) == 0 {
			continue
		}
		path, err := binPath(b)
		if err != nil {
			return &Result{Mutant: m, Status: NotViable, Elapsed: time.Since(start)}
		}
		index := m.Index
		if path != b.path {
			index = 0
		}
		for len(remaining) > 0 {
			quoted := make([]string, len(remaining))
			for i, t := range remaining {
				quoted[i] = regexp.QuoteMeta(t.local)
			}
			limit := time.Duration(float64(total(remaining))*opts.TimeoutFactor) + opts.TimeoutMin
			runCtx, cancel := context.WithTimeout(ctx, limit)
			cmd := testCommand(runCtx, b.dir, path, opts.TestFlags, []string{"GAMUT_MUTANT=" + strconv.Itoa(index)},
				"-test.run", "^(?:"+strings.Join(quoted, "|")+")$", "-test.count=1", "-test.v", "-test.paniconexit0")
			out, err := cmd.CombinedOutput()
			timedOut := runCtx.Err() != nil && ctx.Err() == nil
			cancel()
			if timedOut || ctx.Err() != nil {
				return &Result{Mutant: m, Status: TimedOut, Matrix: true, Elapsed: time.Since(start)}
			}
			started, finished := map[string]bool{}, map[string]bool{}
			for _, f := range verboseLine.FindAllSubmatch(out, -1) {
				if len(f[1]) > 0 {
					started[string(f[1])] = true
					continue
				}
				finished[string(f[3])] = true
				if string(f[2]) == "FAIL" {
					killers[b.prefix+string(f[3])] = true
				}
			}
			var next []*testCase
			for _, t := range remaining {
				switch {
				case !started[t.local]:
					next = append(next, t)
				case !finished[t.local] && err != nil:
					// The process died without printing a result line.
					killers[t.name] = true
				}
			}
			if err != nil && len(next) == len(remaining) {
				// It died before any test started (such as a failing TestMain). Rather than checking the rest one by one, the first test counts as the detector.
				killers[remaining[0].name] = true
				next = next[1:]
			}
			remaining = next
		}
	}
	if len(killers) > 0 {
		r.Status, r.Reason = Killed, ""
		r.Killers = slices.Sorted(maps.Keys(killers))
		for _, t := range tests {
			if killers[t.name] {
				r.Killer, r.KilledBy = t.name, t.name
				break
			}
		}
	}
	r.Elapsed = time.Since(start)
	return r
}

// standalone judges a mutant that could not be embedded in the schemata by building it standalone per test binary.
// Each build happens when it is the turn of that test binary's tests to run.
func (u *unit) standalone(ctx context.Context, m *mutant.Mutant, tests []*testCase, opts Options) *Result {
	name := "standalone-" + strconv.Itoa(m.Index)
	overlay, err := writeOverlay(u.work, name, map[string][]byte{m.File.Path: m.Apply(m.File.Src)})
	if err != nil {
		return &Result{Mutant: m, Status: NotViable, Mode: ModeStandalone}
	}
	var built []string
	defer func() {
		for _, p := range built {
			_ = os.Remove(p)
		}
	}()
	paths := map[*binary]string{}
	r := u.execute(ctx, m, tests, opts, func(b *binary) (string, error) {
		if p, ok := paths[b]; ok {
			return p, nil
		}
		p := filepath.Join(u.work, name+"-"+strconv.Itoa(slices.Index(u.bins, b))+".test")
		// Mutants the compiler rejects despite passing type-checking (such as constant overflow) are not viable.
		if err := goTestC(ctx, b.dir, u.Pkg.ImportPath, u.Pkg.Tags, overlay, p); err != nil {
			return "", err
		}
		built = append(built, p)
		paths[b] = p
		return p, nil
	})
	r.Mode = ModeStandalone
	return r
}
