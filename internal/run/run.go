// Package run builds the schemata test binaries, records coverage, and judges mutants.
package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/schemata"
)

// Status is a verdict's status. These are the internal and summary names; reports map them to mutation-testing-elements statuses (report.MTE).
type Status string

// The statuses of verdicts.
const (
	Killed     Status = "KILLED"
	Lived      Status = "LIVED"
	NotCovered Status = "NOT COVERED"
	NotViable  Status = "NOT VIABLE"
	TimedOut   Status = "TIMED OUT"
	// Pending means the mutant is outside the selection (a diff or identifiers) and was not judged.
	Pending Status = "PENDING"
	// Equivalent means the surviving mutant compiles to the same code as the original, so no test can detect it.
	Equivalent Status = "EQUIVALENT"
)

// The reasons for survival.
const (
	ReasonNotInfected = "not infected"
	ReasonNotDetected = "not detected"
	// ReasonSameCode is the reason for EQUIVALENT.
	ReasonSameCode = "same machine code"
)

// The modes of judging.
const (
	ModeSchemata   = "schemata"
	ModeStandalone = "standalone"
)

// Result is the verdict of one mutant.
type Result struct {
	Mutant   *mutant.Mutant
	Status   Status
	Reason   string
	Mode     string
	Tests    []string
	KilledBy string
	// Killer is the name of the detecting top-level test. KilledBy may include a subtest's name.
	Killer  string
	Elapsed time.Duration
	// Cached reports whether the verdict was reused from the cache without running.
	Cached bool
	// Matrix reports whether the verdict ran every infected test (kill matrix). When true, Killers are all the detecting tests.
	Matrix bool
	// Killers are, when Matrix is true, the names of the detecting top-level tests, in name order.
	Killers []string
	// PreviouslyKilledBy is the previous detecting test, when that test is gone, the mutant was rechecked, and this time it was not detected.
	PreviouslyKilledBy string
}

// Options are the settings of a run.
type Options struct {
	Workers int
	// TimeoutFactor is the factor applied to the baseline running time of the selected tests.
	TimeoutFactor float64
	// TimeoutMin is the fixed time added to the timeout.
	TimeoutMin time.Duration
	// Log is where progress goes, or nowhere when nil.
	Log io.Writer
	// module is the path of the target module, set by All to tell apart the functions whose depth is counted.
	module string
	// When MaxDepth is positive, coverage recording does not count reach whose depth of module function calls from the test function to the mutation site exceeds it.
	MaxDepth int
	// DepthExcluded receives, per package, the number of tests the depth limit removed from reach and the number of removed reaches (pairs of a test and a mutation site).
	// It is called after coverage recording.
	DepthExcluded func(pkg string, tests, reaches int)
	// Unrepeatable receives, per package, the names of the tests that failed on their second run in the same process. It is called after coverage recording.
	// Packages proceed in parallel, so it may be called from several goroutines.
	Unrepeatable func(pkg string, tests []string)
	// Warn is where warnings go, or nowhere when nil.
	Warn io.Writer
	// TestFlags are the flags passed to every run of a test binary. ParseTestFlags makes them.
	TestFlags []string
	// Cache is the cache for reusing verdicts and coverage records. When nil, nothing is reused or recorded.
	// Mutants of a Target without keys (Keys nil) are not reused either.
	Cache *cache.Store
	// Select narrows the mutants to judge. When nil, every mutant is judged.
	Select *Selection
	// Isolate reports whether each mutant is judged in a new test process. When false, test processes that live only during the gamut run
	// judge several mutants one after another (without repeating package initialization per mutant).
	Isolate bool
	// Shard narrows the mutants to judge to one of the groups split by a hash of their identifiers. When nil, all are judged.
	Shard *Shard
	// When Deadline is not zero, no new mutant starts after it, and the rest become PENDING.
	Deadline time.Time
	// KillMatrix reports whether every infected test of each mutant is run and every detecting test recorded (kill matrix).
	// It does not stop at the first failure, and starts a new process per mutant.
	KillMatrix bool

	// rewriteHelper is a hook for tests only that rewrites the helper file's contents before the build.
	rewriteHelper func([]byte) []byte
	// afterJob is a hook for tests only, called each time a mutant finishes running.
	afterJob func()
	// onExecute is a hook for tests only that receives, before a mutant runs, the list of tests per test process.
	onExecute func(m *mutant.Mutant, groups [][]string)
	// noBatch is a hook for tests only that records coverage one test at a time instead of in batches.
	noBatch bool
	// onCoverage is a hook for tests only that receives the names of the tests run to record coverage.
	onCoverage func(names []string)
}

// ErrBaseline means tests fail with mutations disabled, which breaks the premise of judging.
var ErrBaseline = errors.New("baseline tests fail without mutants")

// ErrSelfCheck means tests did not fail with a mutation enabled, showing that mutations do not reach the tests.
// Judging anyway would make every mutant a survivor, indistinguishable from weak tests.
var ErrSelfCheck = errors.New("forced-fail self-check failed")

// Target is one package to judge and its mutants.
type Target struct {
	Pkg     *load.Package
	Mutants []*mutant.Mutant
	// Keys are the keys for reusing verdicts and coverage records. When nil, this package's records are not reused.
	Keys *cache.Keys
	// RevDeps are the reverse dependencies tried for detecting mutations (integration mode).
	RevDeps []RevDep
}

// RevDep is a reverse dependency tried for detecting mutations in integration mode.
type RevDep struct {
	load.RevDep
	// Key is the test key from cache.RevDepKey. When empty, coverage records are not reused.
	Key string
}

// Run judges the mutants of one package.
func Run(ctx context.Context, pkg *load.Package, mutants []*mutant.Mutant, opts Options) ([]*Result, error) {
	results, err := All(ctx, []Target{{Pkg: pkg, Mutants: mutants}}, opts)
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

// unit is the state of one package within All.
type unit struct {
	Target
	results []*Result
	work    string
	sch     *schemata.Result
	// overlay is the overlay file passing the schemata rewrite.
	overlay string
	// bins are the test binaries that may reach mutation sites; bins[0] is the package's own.
	bins []*binary
	// tests are the tests of every test binary.
	tests []*testCase
	// buildTime is the time the build of the package's own test binary took. It feeds the estimates of standalone builds.
	buildTime time.Duration
	// served are the names of the package's own tests the judging test can call. When nil, a process is started per mutant.
	served map[string]bool
	// batches is the number of times reverse dependency test binaries were built together. It names the output directories.
	batches int
	// changed are the names of the tests the diff changed (--since and --include-test-changes).
	changed map[string]bool
}

type job struct {
	unit  *unit
	index int
	run   func() *Result
	// mode is the judging mode, reached the tests that reached the mutation site, and ran the tests run for judging.
	mode    string
	reached []*testCase
	ran     []*testCase
	// forced means the mutant was selected by identifier, so the cache is not used.
	forced bool
	// previousKiller is the previous detecting test, when the mutant is rechecked because that test is gone.
	previousKiller string
	// estimate is the estimated running time.
	estimate time.Duration
}

// All judges every mutant of targets and returns the lists of verdicts in the order of targets.
// It builds the packages in parallel, and puts coverage recording and mutant runs of all packages in one work queue sharing the workers.
// If the baseline tests or the self-check fail in any package, no package is judged.
func All(ctx context.Context, targets []Target, opts Options) ([][]*Result, error) {
	if opts.Workers < 1 {
		opts.Workers = 1
	}
	if len(targets) > 0 {
		opts.module = targets[0].Pkg.ModulePath
	}
	// Preparation and self-checks of packages proceed in parallel, so writes to the outputs are serialized.
	var outMu sync.Mutex
	if opts.Log != nil {
		opts.Log = &lockedWriter{mu: &outMu, w: opts.Log}
	}
	if opts.Warn != nil {
		opts.Warn = &lockedWriter{mu: &outMu, w: opts.Warn}
	}
	logf := func(format string, args ...any) {
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, format+"\n", args...)
		}
	}
	root, err := os.MkdirTemp("", "gamut-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(root) }()

	units := make([]*unit, len(targets))
	var active []*unit
	for i, t := range targets {
		u := &unit{Target: t, results: make([]*Result, len(t.Mutants)), work: filepath.Join(root, strconv.Itoa(i))}
		units[i] = u
		if !opts.Select.touches(u) {
			// A package the selection does not touch is not built, and all its mutants become PENDING.
			for k, m := range t.Mutants {
				u.results[k] = &Result{Mutant: m, Status: Pending}
			}
			continue
		}
		viable := 0
		for k, m := range t.Mutants {
			switch {
			case !opts.Shard.Contains(m.ID):
				u.results[k] = &Result{Mutant: m, Status: Pending}
			case !opts.Select.direct(u, m) && !opts.Select.mayRecheck():
				u.results[k] = &Result{Mutant: m, Status: Pending}
			case !m.Viable:
				u.results[k] = &Result{Mutant: m, Status: NotViable}
				if !opts.Select.direct(u, m) {
					u.results[k].Status = Pending
				}
			default:
				viable++
				if !t.Pkg.HasTests && len(t.RevDeps) == 0 {
					// Without test files no test reaches it, so it is judged without a build.
					u.results[k] = &Result{Mutant: m, Status: NotCovered}
					if !opts.Select.direct(u, m) {
						u.results[k].Status = Pending
					}
				}
			}
		}
		switch {
		case viable > 0 && (t.Pkg.HasTests || len(t.RevDeps) > 0):
			if slices.ContainsFunc(u.results, func(r *Result) bool { return r == nil }) {
				active = append(active, u)
			}
		case viable > 0:
			logf("skip: %s has no test files, so %d viable mutants are not covered", t.Pkg.ImportPath, viable)
		}
	}

	errs := make([]error, len(active))
	parallel(len(active), opts.Workers, func(i int) { errs[i] = prepare(ctx, active[i], opts, logf) })
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	var srv *pool
	if !opts.Isolate && !opts.KillMatrix {
		srv = newPool(ctx, opts)
		defer srv.close()
	}
	// The self-check happens within coverage recording. When a restored test can do the check, it runs alongside measurement.
	if err := cover(ctx, active, opts, srv, logf); err != nil {
		return nil, err
	}
	var all []job
	for _, u := range active {
		all = append(all, u.jobs(ctx, opts, srv)...)
	}
	jobs := all
	if opts.Cache != nil {
		jobs = reuse(all, opts.Cache, duplicateIDs(targets), opts.KillMatrix)
		logf("cache: %d mutants reused, %d to execute", len(all)-len(jobs), len(jobs))
	}
	err = buildLazy(ctx, active, func(u *unit) []*testCase {
		var ts []*testCase
		for _, j := range jobs {
			if j.unit == u && j.mode == ModeSchemata {
				ts = append(ts, j.ran...)
			}
		}
		return ts
	}, opts, logf)
	if err != nil {
		return nil, err
	}
	orderJobs(jobs)
	start := time.Now()
	var mu sync.Mutex
	done := 0
	overBudget := 0
	parallel(len(jobs), opts.Workers, func(k int) {
		j := jobs[k]
		if !opts.Deadline.IsZero() && time.Now().After(opts.Deadline) {
			// Past the budget, do not start and do not write to the cache. The next run continues from here.
			mu.Lock()
			j.unit.results[j.index] = &Result{Mutant: j.unit.Mutants[j.index], Status: Pending}
			overBudget++
			mu.Unlock()
			return
		}
		r := j.run()
		// A run that finished after cancellation cannot tell a failing test from a stopped one, so it is not recorded.
		if ctx.Err() == nil {
			j.record(opts.Cache, r)
		}
		if j.previousKiller != "" && r.Status != Killed && r.Status != TimedOut {
			r.PreviouslyKilledBy = j.previousKiller
		}
		mu.Lock()
		j.unit.results[j.index] = r
		done++
		if opts.Log != nil && (done%50 == 0 || done == len(jobs)) {
			logf("mutants: %d/%d executed", done, len(jobs))
		}
		mu.Unlock()
		if opts.afterJob != nil {
			opts.afterJob()
		}
	})
	logf("execute: %d mutants in %s", len(jobs)-overBudget, time.Since(start).Round(time.Millisecond))
	if overBudget > 0 {
		logf("time budget: exhausted; %d mutants left pending for the next run", overBudget)
	}

	out := make([][]*Result, len(units))
	for i, u := range units {
		out[i] = u.results
	}
	return out, ctx.Err()
}

// orderJobs orders the mutant runs by estimated time, longest first (the LPT rule).
// Starting long mutants first makes it less likely that one long mutant is left running alone at the end of the parallel run. The measurements are in the design's "Execution order".
func orderJobs(jobs []job) {
	slices.SortStableFunc(jobs, func(a, b job) int { return int(b.estimate - a.estimate) })
}

// lockedWriter writes to w holding mu. Log and Warn share mu so their lines do not mix even when they go to the same output.
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// jobs writes the verdicts of the mutants the coverage records decide, and returns the jobs of the mutants that need running.
// Mutants outside the selection become PENDING.
func (u *unit) jobs(ctx context.Context, opts Options, srv *pool) []job {
	var jobs []job
	for i, m := range u.Mutants {
		if u.results[i] != nil {
			continue
		}
		site := u.sch.SiteOf[m]
		var reached, infected []*testCase
		if site.Level != schemata.Untouched {
			for _, t := range u.tests {
				if t.hits[site.Index] {
					reached = append(reached, t)
					if t.infected[m.Index] {
						infected = append(infected, t)
					}
				}
			}
		}
		var j *job
		var res *Result
		switch {
		case site.Level == schemata.Untouched && len(u.standaloneTests()) == 0:
			res = &Result{Mutant: m, Status: NotCovered, Mode: ModeStandalone}
		case site.Level == schemata.Untouched:
			ran := u.standaloneTests()
			j = &job{mode: ModeStandalone, ran: ran, estimate: u.buildTime + total(ran), run: func() *Result {
				return u.standalone(ctx, m, ran, opts)
			}}
		case len(reached) == 0:
			res = &Result{Mutant: m, Status: NotCovered, Mode: modeOf(site)}
		case site.Level == schemata.Marked:
			j = &job{mode: ModeStandalone, reached: reached, ran: reached, estimate: u.buildTime + total(reached), run: func() *Result {
				r := u.standalone(ctx, m, reached, opts)
				r.Tests = testNames(reached)
				return r
			}}
		case len(infected) == 0:
			res = &Result{Mutant: m, Status: Lived, Reason: ReasonNotInfected, Mode: ModeSchemata, Tests: testNames(reached)}
		default:
			j = &job{mode: ModeSchemata, reached: reached, ran: infected, estimate: total(infected), run: func() *Result {
				var r *Result
				if srv != nil && u.canServe(infected) && !slices.ContainsFunc(infected, func(t *testCase) bool { return t.unrepeatable }) {
					r = u.serve(ctx, m, infected, opts, srv)
				} else {
					r = u.execute(ctx, m, infected, opts, func(b *binary) (string, error) { return b.path, nil })
				}
				r.Mode, r.Tests = ModeSchemata, testNames(reached)
				return r
			}}
		}
		// For mutants that run every test in a standalone build, the tests run count as the reaching tests for selection.
		touching := reached
		if j != nil && j.reached == nil {
			touching = j.ran
		}
		selected, previous := opts.Select.selects(u, m, touching, opts.Cache)
		switch {
		case !selected:
			u.results[i] = &Result{Mutant: m, Status: Pending}
		case j != nil:
			j.unit, j.index, j.previousKiller = u, i, previous
			j.forced = opts.Select != nil && opts.Select.Mutants != nil
			jobs = append(jobs, *j)
		default:
			// Verdicts decided without running (not reached, not infected) are all undetected.
			res.PreviouslyKilledBy = previous
			u.results[i] = res
		}
	}
	return jobs
}

func total(ts []*testCase) time.Duration {
	var d time.Duration
	for _, t := range ts {
		d += t.duration
	}
	return d
}

func testNames(ts []*testCase) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.name
	}
	return out
}

func parallel(n, workers int, fn func(int)) {
	ch := make(chan int)
	var wg sync.WaitGroup
	for range min(workers, max(n, 1)) {
		wg.Go(func() {
			for k := range ch {
				fn(k)
			}
		})
	}
	for k := range n {
		ch <- k
	}
	close(ch)
	wg.Wait()
}

func modeOf(s *schemata.Site) string {
	if s.Level == schemata.Wrapped {
		return ModeSchemata
	}
	return ModeStandalone
}
