package run

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/schemata"
)

// binary is one test binary linking the rewrite of the target package.
type binary struct {
	// pkg is the import path of the package holding the tests. prefix is put before test names, and is empty for the package itself.
	pkg    string
	prefix string
	dir    string
	path   string
	// revdep holds the reverse dependency's information for a reverse dependency, and is nil for the package itself.
	revdep *RevDep
	// key is the test key of the reverse dependency.
	key   string
	names []string
	tests []*testCase
	// lazy means the coverage records of every test can be restored, so the test list comes from the records and the build waits until needed.
	lazy bool
	// built reports whether the test binary has been built.
	built bool
	// batchable are the names, within the test binary, of the tests whose coverage can be recorded together in one process.
	batchable map[string]bool
}

type testCase struct {
	bin *binary
	// name is the name used in reports and the cache, and local the name within the test binary.
	name, local string
	duration    time.Duration
	hits        map[int]bool
	infected    map[int]bool
	// entered are the numbers of the functions entered (the numbers of schemata.Result.Funcs).
	entered map[int]bool
	// key is the test's key.
	key string
	// measured reports whether coverage was measured in this run. When false, it was restored from the cache records.
	measured bool
	// unrepeatable reports whether the test failed on its second run in the same process. Mutants involving such tests are judged without reusing processes.
	unrepeatable bool
	// deep are the numbers of the mutation sites reached beyond the depth limit (Options.MaxDepth). They do not count as reached.
	deep map[int]bool
}

func (t *testCase) markDeep(site int) {
	if t.deep == nil {
		t.deep = map[int]bool{}
	}
	t.deep[site] = true
}

// deepOnly returns the number of mutation sites reached only beyond the depth limit.
func (t *testCase) deepOnly() int {
	n := 0
	for site := range t.deep {
		if !t.hits[site] {
			n++
		}
	}
	return n
}

func newTestCase(b *binary, local, key string) *testCase {
	return &testCase{bin: b, name: b.prefix + local, local: local, key: key,
		hits: map[int]bool{}, infected: map[int]bool{}, entered: map[int]bool{}}
}

// test returns the test named name, or nil when there is none.
func (u *unit) test(name string) *testCase {
	for _, t := range u.tests {
		if t.name == name {
			return t
		}
	}
	return nil
}

// standaloneTests returns the tests run for standalone-built mutants without reach records.
// They are every test of the package itself, and every test of the reverse dependency test binaries that reached a mutation site of the target package.
func (u *unit) standaloneTests() []*testCase {
	var out []*testCase
	for i, b := range u.bins {
		if i == 0 || slices.ContainsFunc(b.tests, func(t *testCase) bool { return len(t.hits) > 0 }) {
			out = append(out, b.tests...)
		}
	}
	return out
}

// prepare does the schemata rewrite of the package, builds the test binary, and gets the test list.
// The test binaries of reverse dependencies are built with the same rewrite.
func prepare(ctx context.Context, u *unit, opts Options, logf func(string, ...any)) error {
	pkg := u.Pkg
	sch, err := schemata.Build(pkg, u.Mutants)
	if err != nil {
		return err
	}
	u.sch = sch
	notViable := 0
	for _, m := range u.Mutants {
		if !m.Viable {
			notViable++
		}
	}
	logf("schemata: %s: %d embedded, %d standalone with coverage, %d standalone without coverage, %d not viable", pkg.ImportPath,
		sch.Count(schemata.Wrapped), sch.Count(schemata.Marked), sch.Count(schemata.Untouched), notViable)

	if err := os.MkdirAll(u.work, 0o700); err != nil {
		return err
	}
	helper := sch.Helper
	if opts.rewriteHelper != nil {
		helper = opts.rewriteHelper(helper)
	}
	files := map[string][]byte{sch.HelperPath: helper}
	maps.Copy(files, sch.Files)
	marks, err := schemata.MarkTests(pkg)
	if err != nil {
		return err
	}
	maps.Copy(files, marks.Files)
	if !opts.Isolate && !opts.KillMatrix {
		srv, err := schemata.BuildServer(pkg)
		if err != nil {
			return err
		}
		if srv != nil {
			files[srv.Path] = srv.Src
			u.served = srv.Tests
		}
	}
	if u.overlay, err = writeOverlay(u.work, "schemata", files); err != nil {
		return err
	}
	own := &binary{pkg: pkg.ImportPath, dir: pkg.Dir, path: filepath.Join(u.work, "schemata.test"), batchable: marks.Batchable}
	u.bins = []*binary{own}
	switch {
	case !pkg.HasTests:
	case u.ownRestorable(own, opts.Cache):
		// When every test's record can be restored, do not build until coverage is measured or a mutant is run.
		own.lazy = true
	default:
		if err := u.buildOwn(ctx, logf); err != nil {
			return err
		}
		if !u.sameTestSet(own, opts.Cache) {
			if own.names, err = listTests(ctx, own.dir, own.path, opts.TestFlags); err != nil {
				return err
			}
		}
	}
	var eager []*binary
	for i := range u.RevDeps {
		r := &u.RevDeps[i]
		b := &binary{pkg: r.ImportPath, prefix: r.ImportPath + ".", dir: r.Dir, revdep: r, key: r.Key,
			path: filepath.Join(u.work, "revdep-"+strconv.Itoa(i)+".test")}
		u.bins = append(u.bins, b)
		if u.restorable(b, opts.Cache) {
			b.lazy = true
		} else {
			eager = append(eager, b)
		}
	}
	if err := u.build(ctx, eager, logf); err != nil {
		return err
	}
	errs := make([]error, len(eager))
	parallel(len(eager), opts.Workers, func(i int) {
		eager[i].names, errs[i] = listTests(ctx, eager[i].dir, eager[i].path, opts.TestFlags)
	})
	return errors.Join(errs...)
}

// buildOwn builds the test binary of the package itself.
func (u *unit) buildOwn(ctx context.Context, logf func(string, ...any)) error {
	own := u.bins[0]
	start := time.Now()
	if err := goTestC(ctx, own.dir, u.Pkg.ImportPath, u.Pkg.Tags, u.overlay, own.path); err != nil {
		return err
	}
	own.built = true
	u.buildTime = time.Since(start)
	logf("build: %s in %s", u.Pkg.ImportPath, u.buildTime.Round(time.Millisecond))
	return nil
}

// ownRestorable reports whether the coverage records of every test of the package's own test binary can be restored.
// If so, the test list comes from the records. Getting the test list otherwise needs a build, so this holds only
// when the set of recorded tests equals the set of top-level test functions in the test files and every test's key equals its record's.
func (u *unit) ownRestorable(own *binary, store *cache.Store) bool {
	if store == nil || u.Keys == nil {
		return false
	}
	cov := store.Coverage(u.Pkg.ImportPath, own.pkg)
	funcs := u.Keys.TestFuncs()
	if cov == nil || len(cov.Tests) != len(funcs) {
		return false
	}
	r := u.restorer(cov)
	for _, name := range funcs {
		rec := cov.Tests[name]
		if rec == nil || rec.Key != u.Keys.Test(name) || !r.valid(rec) {
			return false
		}
	}
	own.names = funcs
	return true
}

// sameTestSet reports whether the set of recorded tests equals the set of top-level test functions in the test files.
// If so, that set becomes the list instead of starting the test binary to get one.
func (u *unit) sameTestSet(own *binary, store *cache.Store) bool {
	if store == nil || u.Keys == nil {
		return false
	}
	cov := store.Coverage(u.Pkg.ImportPath, own.pkg)
	funcs := u.Keys.TestFuncs()
	if cov == nil || len(cov.Tests) != len(funcs) {
		return false
	}
	for _, name := range funcs {
		if cov.Tests[name] == nil {
			return false
		}
	}
	own.names = funcs
	return true
}

// build builds the reverse dependency test binaries bins with the target package's rewrite applied.
// Starting the go command per package would reload the dependency graph every time and tie the parallelism to the number of workers.
// So several packages are passed together to go test -c -o <dir>/, letting the go command build in parallel on every CPU.
// The output names come from the last element of the import path, so packages with the same name go into separate rounds.
func (u *unit) build(ctx context.Context, bins []*binary, logf func(string, ...any)) error {
	var batches [][]*binary
	for _, b := range bins {
		name := pathpkg.Base(b.pkg)
		placed := false
		for i, batch := range batches {
			if !slices.ContainsFunc(batch, func(o *binary) bool { return pathpkg.Base(o.pkg) == name }) {
				batches[i] = append(batch, b)
				placed = true
				break
			}
		}
		if !placed {
			batches = append(batches, []*binary{b})
		}
	}
	type batchJob struct {
		bins []*binary
		out  string
	}
	jobs := make([]batchJob, len(batches))
	for i, batch := range batches {
		u.batches++
		jobs[i] = batchJob{batch, filepath.Join(u.work, "revdeps-"+strconv.Itoa(u.batches))}
	}
	buildBatch := func(j batchJob) error {
		if err := os.MkdirAll(j.out, 0o700); err != nil {
			return err
		}
		pkgs := make([]string, len(j.bins))
		for i, b := range j.bins {
			pkgs[i] = b.pkg
		}
		start := time.Now()
		args := append([]string{"test", "-c", "-o", j.out + string(filepath.Separator), "-overlay", u.overlay}, buildFlags(u.Pkg.ImportPath)...)
		if len(u.Pkg.Tags) > 0 {
			args = append(args, "-tags="+strings.Join(u.Pkg.Tags, ","))
		}
		cmd := exec.CommandContext(ctx, "go", append(args, pkgs...)...)
		cmd.Dir = u.Pkg.ModuleDir
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("build test binaries of %s: %w\n%s", strings.Join(pkgs, " "), err, output)
		}
		for _, b := range j.bins {
			b.path = filepath.Join(j.out, pathpkg.Base(b.pkg)+".test")
			b.built = true
		}
		logf("build: %s with %s rewritten in %s", strings.Join(pkgs, " "), u.Pkg.ImportPath, time.Since(start).Round(time.Millisecond))
		return nil
	}
	if len(jobs) == 0 {
		return nil
	}
	// The first (largest) batch puts the shared dependencies in the build cache. The remaining batches are mostly linking, so they run at once, up to the number of CPUs.
	if err := buildBatch(jobs[0]); err != nil {
		return err
	}
	rest := jobs[1:]
	errs := make([]error, len(rest))
	parallel(len(rest), runtime.NumCPU(), func(i int) { errs[i] = buildBatch(rest[i]) })
	return errors.Join(errs...)
}

// restorable reports whether the coverage records of every test of the reverse dependency test binary b can be restored.
// If so, the test list comes from the records. The key is per package, so the same key means the same test list.
// The build is delayed until coverage is measured or a mutant is run. A package that no test reaches is never built.
func (u *unit) restorable(b *binary, store *cache.Store) bool {
	if store == nil || u.Keys == nil || b.key == "" {
		return false
	}
	cov := store.Coverage(u.Pkg.ImportPath, b.pkg)
	if cov == nil || len(cov.Tests) == 0 {
		return false
	}
	r := u.restorer(cov)
	for _, rec := range cov.Tests {
		if rec.Key != b.key || !r.valid(rec) {
			return false
		}
	}
	b.names = slices.Sorted(maps.Keys(cov.Tests))
	return true
}

// buildLazy builds the test binaries tests uses that are not built yet.
func buildLazy(ctx context.Context, units []*unit, tests func(u *unit) []*testCase, opts Options, logf func(string, ...any)) error {
	errs := make([]error, len(units))
	parallel(len(units), opts.Workers, func(i int) {
		u := units[i]
		var todo []*binary
		own := false
		for _, t := range tests(u) {
			b := t.bin
			switch {
			case !b.lazy || b.built:
			case b == u.bins[0]:
				own = true
			case !slices.Contains(todo, b):
				todo = append(todo, b)
			}
		}
		if own {
			if errs[i] = u.buildOwn(ctx, logf); errs[i] != nil {
				return
			}
		}
		errs[i] = u.build(ctx, todo, logf)
	})
	return errors.Join(errs...)
}

// writeOverlay writes files under work and returns the path of the overlay file that replaces the original files with them.
func writeOverlay(work, name string, files map[string][]byte) (string, error) {
	srcDir := filepath.Join(work, name+"-src")
	if err := os.MkdirAll(srcDir, 0o700); err != nil {
		return "", err
	}
	replace := map[string]string{}
	for i, path := range slices.Sorted(maps.Keys(files)) {
		dst := filepath.Join(srcDir, strconv.Itoa(i)+"_"+filepath.Base(path))
		if err := os.WriteFile(dst, files[path], 0o600); err != nil {
			return "", err
		}
		replace[path] = dst
	}
	overlay, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return "", err
	}
	overlayPath := filepath.Join(work, name+"-overlay.json")
	return overlayPath, os.WriteFile(overlayPath, overlay, 0o600)
}

var (
	buildOnce sync.Once
	goFlags   string
)

// buildFlags returns the flags added to the build of the test binary of the package importPath. None of them changes the verdicts, and all shorten the build.
//   - -vet=off: go test -c runs part of go vet, which judging does not use (about 20% for one package of idmagic).
//   - -gcflags=<importPath>=-l: the rewritten package is large, with a branch per mutant, so inlining is turned off for that package only (also about 20%).
//     The flags of dependencies do not change, so dependencies come from the build cache as is.
//   - -ldflags=-s -w: test binaries use neither the symbol table nor debug information (DWARF), so linking gets shorter (also about 20%).
//     Stack traces and runtime.FuncForPC use another table (pclntab), so they are unchanged.
//
// When the user sets the same flag in GOFLAGS, it is not added, so as not to override it.
func buildFlags(importPath string) []string {
	buildOnce.Do(func() {
		if out, err := exec.Command("go", "env", "GOFLAGS").Output(); err == nil {
			goFlags = string(out)
		}
	})
	var flags []string
	if !strings.Contains(goFlags, "-vet") {
		flags = append(flags, "-vet=off")
	}
	if !strings.Contains(goFlags, "-gcflags") {
		flags = append(flags, "-gcflags="+importPath+"=-l")
	}
	if !strings.Contains(goFlags, "-ldflags") {
		flags = append(flags, "-ldflags=-s -w")
	}
	return flags
}

// goTestC builds the test binary of the package in dir to bin with the overlay applied.
func goTestC(ctx context.Context, dir, importPath string, tags []string, overlay, bin string) error {
	args := append([]string{"test", "-c", "-o", bin, "-overlay", overlay}, buildFlags(importPath)...)
	if len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, ","))
	}
	cmd := exec.CommandContext(ctx, "go", append(args, ".")...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build test binary in %s: %w\n%s", dir, err, out)
	}
	return nil
}

// testCommand builds the command that starts a test binary.
// The user's flags come first, and the flags gamut sets per run after them.
// The test binary starts in a new process group, so cancellation or a timeout stops it together with the child processes the test started.
func testCommand(ctx context.Context, dir, bin string, flags, env []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, append(slices.Clone(flags), args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	// Even after stopping, a grandchild process may keep holding the output pipe, so the wait is bounded.
	cmd.WaitDelay = time.Second
	isolate(cmd)
	return cmd
}

var runnable = regexp.MustCompile(`^(Test|Example|Fuzz)`)

func listTests(ctx context.Context, dir, bin string, flags []string) ([]string, error) {
	cmd := testCommand(ctx, dir, bin, flags, nil, "-test.list", ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// A failing setup in TestMain also prevents getting the list.
		return nil, fmt.Errorf("%w: listing tests in %s failed: %w\n%s%s", ErrBaseline, dir, err, out, stderr.Bytes())
	}
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		name := strings.TrimSpace(sc.Text())
		if runnable.MatchString(name) && name != schemata.ServerTestName {
			names = append(names, name)
		}
	}
	return names, nil
}

// cover restores from the cache the test coverage of every package it can, measures the rest in one work queue, and writes the records back.
// When p is not nil, the processes that measured coverage are left in p for judging mutants.
func cover(ctx context.Context, active []*unit, opts Options, p *pool, logf func(string, ...any)) error {
	var measure []*testCase
	restored := 0
	for _, u := range active {
		for _, t := range u.plan(opts) {
			if t.measured {
				measure = append(measure, t)
			} else {
				restored++
			}
		}
	}
	if opts.onCoverage != nil {
		opts.onCoverage(testNames(measure))
	}
	if err := buildLazy(ctx, active, func(*unit) []*testCase { return measure }, opts, logf); err != nil {
		return err
	}
	// Packages whose restored tests have reach start the self-check now, overlapping it with coverage measurement.
	early := make([]bool, len(active))
	probes := make([]*testCase, len(active))
	for i, u := range active {
		if u.bins[0].built {
			probes[i] = shortestReaching(u.bins[0].tests, func(t *testCase) bool { return !t.measured })
		}
		early[i] = probes[i] != nil
	}
	checkErrs := make([]error, len(active))
	var checks sync.WaitGroup
	checks.Go(func() {
		var idx []int
		for i := range active {
			if early[i] {
				idx = append(idx, i)
			}
		}
		parallel(len(idx), opts.Workers, func(k int) { checkErrs[idx[k]] = selfCheckWith(ctx, active[idx[k]], probes[idx[k]], opts) })
	})
	start := time.Now()
	total := len(measure)
	measure = probeRevDeps(ctx, measure, opts)
	if !opts.noBatch {
		measure = coverBatches(ctx, active, measure, opts, p)
	}
	errs := make([]error, len(measure))
	parallel(len(measure), opts.Workers, func(k int) {
		errs[k] = coverTest(ctx, measure[k], filepath.Join(measure[k].bin.dirWork(), "cover-"+strconv.Itoa(k)), opts)
	})
	checks.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}
	parallel(len(active), opts.Workers, func(i int) {
		if !early[i] {
			checkErrs[i] = selfCheck(ctx, active[i], opts)
		}
	})
	if err := errors.Join(checkErrs...); err != nil {
		return err
	}
	for _, u := range active {
		u.changed = opts.Select.changedTests(u)
		u.saveCoverage(opts.Cache)
		if opts.DepthExcluded != nil {
			tests, reaches := 0, 0
			for _, t := range u.tests {
				if n := t.deepOnly(); n > 0 {
					tests++
					reaches += n
				}
			}
			if tests > 0 {
				opts.DepthExcluded(u.Pkg.ImportPath, tests, reaches)
			}
		}
		if opts.Unrepeatable != nil {
			var names []string
			for _, t := range u.tests {
				if t.unrepeatable {
					names = append(names, t.name)
				}
			}
			if len(names) > 0 {
				opts.Unrepeatable(u.Pkg.ImportPath, names)
			}
		}
		for _, b := range u.bins[1:] {
			if !slices.ContainsFunc(b.tests, func(t *testCase) bool { return len(t.hits) > 0 }) {
				logf("integration: %s: no test of %s reaches a mutated site", u.Pkg.ImportPath, b.pkg)
			}
		}
		logf("coverage: %s: %d tests", u.Pkg.ImportPath, len(u.tests))
	}
	logf("coverage: %d tests measured (%d one by one), %d restored from the cache in %s", total, len(measure), restored, time.Since(start).Round(time.Millisecond))
	return nil
}

// coverBatches splits the package's own tests that can be measured together into as many chunks as workers, runs each chunk in one process,
// and splits the records at the boundaries inserted at the start of the test functions. It returns the tests it could not split, which are measured one by one.
// Instead of one process per test, it starts one per chunk.
// When p is not nil and the judging test can call every test of a chunk, the chunk is measured in a judging test process, which is then reused for judging.
func coverBatches(ctx context.Context, active []*unit, measure []*testCase, opts Options, p *pool) []*testCase {
	type chunk struct {
		unit  *unit
		tests []*testCase
		ok    bool
	}
	var chunks []*chunk
	var rest []*testCase
	for _, u := range active {
		for _, b := range u.bins {
			var batch []*testCase
			for _, t := range measure {
				switch {
				case t.bin != b:
				case b.batchable[t.local]:
					batch = append(batch, t)
				default:
					rest = append(rest, t)
				}
			}
			if len(batch) < 2 {
				rest = append(rest, batch...)
				continue
			}
			n := min(opts.Workers, len(batch))
			split := make([]*chunk, n)
			for i := range split {
				split[i] = &chunk{unit: u}
			}
			for i, t := range batch {
				split[i%n].tests = append(split[i%n].tests, t)
			}
			chunks = append(chunks, split...)
		}
	}
	parallel(len(chunks), opts.Workers, func(i int) {
		c := chunks[i]
		path := filepath.Join(c.tests[0].bin.dirWork(), "batch-"+strconv.Itoa(i))
		if p != nil && c.unit.canServe(c.tests) {
			c.ok = coverServed(ctx, c.tests, path, opts, p)
		} else {
			c.ok = coverChunk(ctx, c.tests, path, opts)
		}
	})
	for _, c := range chunks {
		if !c.ok {
			rest = append(rest, c.tests...)
		}
	}
	return rest
}

var passLine = regexp.MustCompile(`(?m)^--- (?:PASS|SKIP): (\S+) \(([0-9.]+)s\)`)

// coverChunk runs tests in one process with -test.parallel=1 and splits the record at the boundaries "t <name>".
// If the process fails or a test has no boundary, it returns false and empties the records of tests.
func coverChunk(ctx context.Context, tests []*testCase, path string, opts Options) bool {
	b := tests[0].bin
	quoted := make([]string, len(tests))
	for i, t := range tests {
		quoted[i] = regexp.QuoteMeta(t.local)
	}
	start := time.Now()
	cmd := testCommand(ctx, b.dir, b.path, opts.TestFlags, opts.coverEnv(path, "GAMUT_MUTANT=0"),
		"-test.run", "^(?:"+strings.Join(quoted, "|")+")$", "-test.count=1", "-test.parallel=1", "-test.v", "-test.paniconexit0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	durations := map[string]time.Duration{}
	for _, m := range passLine.FindAllStringSubmatch(string(out), -1) {
		if sec, err := strconv.ParseFloat(m[2], 64); err == nil {
			durations[m[1]] = time.Duration(sec * float64(time.Second))
		}
	}
	return splitRecords(tests, path, durations, time.Since(start))
}

// coverEnv returns the environment variables that record coverage to path, plus extra and the depth limit.
func (o Options) coverEnv(path string, extra ...string) []string {
	env := append([]string{"GAMUT_COVER=" + path}, extra...)
	if o.MaxDepth > 0 {
		env = append(env, "GAMUT_MAX_DEPTH="+strconv.Itoa(o.MaxDepth), "GAMUT_MODULE="+o.module)
	}
	return env
}

// coverServed runs tests without mutations in a judging test process to record coverage, and splits the record at the boundaries.
// On success, the process stops recording coverage and goes back to p for judging mutants, so the tests' variables are initialized once for both coverage and judging.
// On failure, it throws the process away and returns false.
func coverServed(ctx context.Context, tests []*testCase, path string, opts Options, p *pool) bool {
	b := tests[0].bin
	start := time.Now()
	s, err := startServer(ctx, b, opts, opts.coverEnv(path))
	if err != nil {
		return false
	}
	outcome, durations, unrepeatable := s.cover(ctx, testNames(tests))
	if outcome != servedPass {
		s.close()
		return false
	}
	if !splitRecords(tests, path, durations, time.Since(start)) {
		s.close()
		return false
	}
	for _, t := range tests {
		t.unrepeatable = slices.Contains(unrepeatable, t.local)
	}
	p.put(b, s)
	return true
}

// splitRecords splits the record at path among tests at the boundaries "t <name>", and sets durations from durations (or, when missing, an equal share of elapsed).
// Records before the first boundary (package initialization) appear for every test, so they are added to all tests.
// If a test has no boundary, it returns false and empties the records of tests.
func splitRecords(tests []*testCase, path string, durations map[string]time.Duration, elapsed time.Duration) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	byName := map[string]*testCase{}
	for _, t := range tests {
		byName[t.local] = t
	}
	common := &testCase{hits: map[int]bool{}, infected: map[int]bool{}, entered: map[int]bool{}}
	cur := common
	seen := map[*testCase]bool{}
	for line := range strings.Lines(string(data)) {
		kind, arg, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		if kind == "t" {
			if cur = byName[arg]; cur == nil {
				break
			}
			seen[cur] = true
			continue
		}
		n, convErr := strconv.Atoi(arg)
		if convErr != nil {
			continue
		}
		switch kind {
		case "h":
			cur.hits[n] = true
		case "i":
			cur.infected[n] = true
		case "e":
			cur.entered[n] = true
		case "d":
			cur.markDeep(n)
		}
	}
	if cur == nil || len(seen) != len(tests) {
		for _, t := range tests {
			t.hits, t.infected, t.entered, t.deep = map[int]bool{}, map[int]bool{}, map[int]bool{}, nil
		}
		return false
	}
	for _, t := range tests {
		maps.Copy(t.hits, common.hits)
		maps.Copy(t.infected, common.infected)
		maps.Copy(t.entered, common.entered)
		for n := range common.deep {
			t.markDeep(n)
		}
		t.duration = durations[t.local]
		if t.duration == 0 {
			t.duration = elapsed / time.Duration(len(tests))
		}
	}
	return true
}

// probeRevDeps runs, per reverse dependency test binary, the tests to measure together in one process,
// and when neither reach of a mutation site nor entry into a function is recorded, takes those tests out of one-by-one measurement with empty records.
// Most reverse dependencies reach no mutation site of the target package, so one process start replaces one per test.
// If the combined run fails, the tests are measured one by one (where a baseline test failure is reported per test).
func probeRevDeps(ctx context.Context, measure []*testCase, opts Options) []*testCase {
	byBin := map[*binary][]*testCase{}
	var bins []*binary
	for _, t := range measure {
		if t.bin.revdep == nil {
			continue
		}
		if byBin[t.bin] == nil {
			bins = append(bins, t.bin)
		}
		byBin[t.bin] = append(byBin[t.bin], t)
	}
	empty := make([]bool, len(bins))
	parallel(len(bins), opts.Workers, func(i int) {
		b := bins[i]
		ts := byBin[b]
		if len(ts) < 2 {
			return
		}
		path := filepath.Join(b.dirWork(), "probe-"+strconv.Itoa(i)+"-"+filepath.Base(b.path))
		quoted := make([]string, len(ts))
		for k, t := range ts {
			quoted[k] = regexp.QuoteMeta(t.local)
		}
		cmd := testCommand(ctx, b.dir, b.path, opts.TestFlags, opts.coverEnv(path, "GAMUT_MUTANT=0"),
			"-test.run", "^(?:"+strings.Join(quoted, "|")+")$", "-test.count=1", "-test.paniconexit0")
		if err := cmd.Run(); err != nil {
			return
		}
		data, err := os.ReadFile(path)
		empty[i] = errors.Is(err, os.ErrNotExist) || err == nil && len(data) == 0
	})
	skip := map[*binary]bool{}
	for i, b := range bins {
		skip[b] = empty[i]
	}
	var out []*testCase
	for _, t := range measure {
		if !skip[t.bin] {
			out = append(out, t)
		}
	}
	return out
}

// dirWork returns the directory for the coverage record files.
func (b *binary) dirWork() string { return filepath.Dir(b.path) }

// plan creates every test of u, restores those whose cache records can be restored, sets measured on those to measure again, and returns them.
//
// A test's record is restored when all of the following hold.
//   - The dependencies outside the package, the initialization declarations, and the rewrite level of every mutation site are the same as when recorded.
//   - The test's key is the same.
//   - Every entered function and every declaration enclosing a reached mutation site in the record still exists unchanged (dependencies included).
//   - The mutation sites and mutants in the record still exist.
//
// When mutants are selected by identifier, tests that reached the mutation sites of the selected mutants are measured again too.
func (u *unit) plan(opts Options) []*testCase {
	selected := map[string]bool{}
	if opts.Select != nil {
		for m := range opts.Select.Mutants {
			selected[m.SiteKey] = true
		}
	}
	for _, b := range u.bins {
		var cov *cache.Coverage
		if opts.Cache != nil && u.Keys != nil {
			cov = opts.Cache.Coverage(u.Pkg.ImportPath, b.pkg)
		}
		r := u.restorer(cov)
		for _, name := range b.names {
			key := b.key
			if b.revdep == nil && u.Keys != nil {
				key = u.Keys.Test(name)
			}
			t := newTestCase(b, name, key)
			var rec *cache.TestRecord
			if cov != nil {
				rec = cov.Tests[name]
			}
			switch {
			case rec != nil && rec.Key == key && key != "" && r.valid(rec) && !slices.ContainsFunc(rec.Hits, func(h string) bool { return selected[h] }):
				r.restore(t, rec)
			default:
				t.measured = true
			}
			b.tests = append(b.tests, t)
			u.tests = append(u.tests, t)
		}
	}
	return u.tests
}

// restorer maps the cached coverage records onto the numbers of the current rewrite.
type restorer struct {
	ok      bool
	reaches map[string]bool
	sites   map[string]*schemata.Site
	mutants map[string]int
	funcs   map[string]int
}

func (u *unit) restorer(cov *cache.Coverage) *restorer {
	r := &restorer{}
	if cov == nil || u.Keys == nil || cov.Imports != u.Keys.Imports() || cov.Init != u.Keys.Init() {
		return r
	}
	r.sites = map[string]*schemata.Site{}
	for _, s := range u.sch.Sites {
		key := s.Mutants[0].SiteKey
		if _, dup := r.sites[key]; dup {
			// Two sites with the same key (such as two init functions of the same shape) make the records indistinguishable.
			return r
		}
		r.sites[key] = s
		if old, ok := cov.Levels[key]; ok && old != int(s.Level) {
			return r
		}
	}
	r.mutants = map[string]int{}
	for _, m := range u.Mutants {
		r.mutants[m.Key()] = m.Index
	}
	r.funcs = map[string]int{}
	for i, fd := range u.sch.Funcs {
		r.funcs[u.Keys.ReachOf(fd)] = i + 1
	}
	r.reaches = u.Keys.Reaches()
	r.ok = true
	return r
}

func (r *restorer) valid(rec *cache.TestRecord) bool {
	if !r.ok || rec == nil {
		return false
	}
	for _, h := range rec.Reach {
		if !r.reaches[h] {
			return false
		}
	}
	for _, h := range rec.Hits {
		if s := r.sites[h]; s == nil || s.Level == schemata.Untouched {
			return false
		}
	}
	for _, k := range rec.Infected {
		if _, ok := r.mutants[k]; !ok {
			return false
		}
	}
	return true
}

func (r *restorer) restore(t *testCase, rec *cache.TestRecord) {
	if rec == nil {
		return
	}
	t.duration = time.Duration(rec.Duration)
	t.unrepeatable = rec.Unrepeatable
	for _, d := range rec.Deep {
		if s := r.sites[d]; s != nil {
			t.markDeep(s.Index)
		}
	}
	for _, h := range rec.Hits {
		t.hits[r.sites[h].Index] = true
	}
	for _, k := range rec.Infected {
		t.infected[r.mutants[k]] = true
	}
	for _, h := range rec.Reach {
		if i, ok := r.funcs[h]; ok {
			t.entered[i] = true
		}
	}
}

// saveCoverage writes the coverage of every test of this run to the cache. Restored tests' records are written back with the same contents.
func (u *unit) saveCoverage(store *cache.Store) {
	if store == nil || u.Keys == nil {
		return
	}
	levels := map[string]int{}
	siteByIndex := map[int]*schemata.Site{}
	for _, s := range u.sch.Sites {
		levels[s.Mutants[0].SiteKey] = int(s.Level)
		if s.Level != schemata.Untouched {
			siteByIndex[s.Index] = s
		}
	}
	mutantByIndex := map[int]string{}
	for _, m := range u.Mutants {
		mutantByIndex[m.Index] = m.Key()
	}
	for _, b := range u.bins {
		cov := &cache.Coverage{Imports: u.Keys.Imports(), Init: u.Keys.Init(), Levels: levels, Tests: map[string]*cache.TestRecord{}}
		for _, t := range b.tests {
			rec := &cache.TestRecord{Key: t.key, Duration: int64(t.duration), Unrepeatable: t.unrepeatable}
			reach := map[string]bool{}
			for i := range t.entered {
				reach[u.Keys.ReachOf(u.sch.Funcs[i-1])] = true
			}
			for i := range t.hits {
				s := siteByIndex[i]
				rec.Hits = append(rec.Hits, s.Mutants[0].SiteKey)
				reach[u.Keys.Reach(s.Mutants[0])] = true
			}
			for i := range t.infected {
				rec.Infected = append(rec.Infected, mutantByIndex[i])
			}
			for i := range t.deep {
				if s := siteByIndex[i]; s != nil && !t.hits[i] {
					rec.Deep = append(rec.Deep, s.Mutants[0].SiteKey)
				}
			}
			slices.Sort(rec.Deep)
			rec.Reach = slices.Sorted(maps.Keys(reach))
			slices.Sort(rec.Hits)
			slices.Sort(rec.Infected)
			cov.Tests[t.local] = rec
		}
		store.PutCoverage(u.Pkg.ImportPath, b.pkg, cov)
	}
}

// coverTest runs t in its own process with mutations disabled, and records reach, infection, and entered functions.
func coverTest(ctx context.Context, t *testCase, path string, opts Options) error {
	b := t.bin
	start := time.Now()
	cmd := testCommand(ctx, b.dir, b.path, opts.TestFlags, opts.coverEnv(path, "GAMUT_MUTANT=0"),
		"-test.run", "^"+regexp.QuoteMeta(t.local)+"$", "-test.count=1", "-test.paniconexit0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s.%s fails, so verdicts would be meaningless: %w\n%s", ErrBaseline, b.pkg, t.local, err, out)
	}
	t.duration = time.Since(start)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for line := range strings.Lines(string(data)) {
		kind, num, ok := strings.Cut(strings.TrimSpace(line), " ")
		n, convErr := strconv.Atoi(num)
		if !ok || convErr != nil {
			continue
		}
		switch kind {
		case "h":
			t.hits[n] = true
		case "i":
			t.infected[n] = true
		case "e":
			t.entered[n] = true
		case "d":
			t.markDeep(n)
		}
	}
	return nil
}

// selfCheck runs, with forced fail, the shortest of the package's own tests that has reach records, and checks that it fails.
// If it passes although reach was recorded, the test process does not read GAMUT_MUTANT.
func selfCheck(ctx context.Context, u *unit, opts Options) error {
	if !u.bins[0].built {
		// Nothing was built only when there is neither a test to measure nor a mutant to run.
		// Every verdict then comes from the records of a previous run that passed the self-check in the same environment.
		return nil
	}
	return selfCheckWith(ctx, u, shortestReaching(u.bins[0].tests, func(*testCase) bool { return true }), opts)
}

// shortestReaching returns the shortest test in tests that satisfies ok and has reach records.
func shortestReaching(tests []*testCase, ok func(*testCase) bool) *testCase {
	var probe *testCase
	for _, t := range tests {
		if ok(t) && len(t.hits) > 0 && (probe == nil || t.duration < probe.duration) {
			probe = t
		}
	}
	return probe
}

// selfCheckWith runs probe with forced fail and checks that it fails. When probe is nil, it warns that the check was skipped.
func selfCheckWith(ctx context.Context, u *unit, probe *testCase, opts Options) error {
	own := u.bins[0]
	pkgPath := u.Pkg.ImportPath
	if probe == nil {
		if opts.Warn != nil {
			fmt.Fprintf(opts.Warn, "gamut: warning: forced-fail self-check skipped for %s because no test reaches a mutated site\n", pkgPath)
		}
		return nil
	}
	limit := time.Duration(float64(probe.duration)*opts.TimeoutFactor) + opts.TimeoutMin
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	start := time.Now()
	// gamutHit is called only while recording coverage, so enable recording and pass forced fail.
	env := []string{"GAMUT_COVER=" + filepath.Join(u.work, "forced-fail"), "GAMUT_MUTANT=" + strconv.Itoa(schemata.ForcedFail)}
	cmd := testCommand(runCtx, own.dir, own.path, opts.TestFlags, env, "-test.run", "^"+regexp.QuoteMeta(probe.local)+"$", "-test.count=1", "-test.paniconexit0")
	out, err := cmd.CombinedOutput()
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case runCtx.Err() != nil:
		return fmt.Errorf("%w: %s.%s did not finish within %s with GAMUT_MUTANT=%d", ErrSelfCheck, pkgPath, probe.name, limit, schemata.ForcedFail)
	case err == nil:
		return fmt.Errorf("%w: %s.%s reaches mutated sites but passed with GAMUT_MUTANT=%d; "+
			"the test process does not read the mutant selection, so every mutant would survive "+
			"(check that GAMUT_MUTANT reaches the test binary and that the code under test does not recover the forced panic)\n%s",
			ErrSelfCheck, pkgPath, probe.name, schemata.ForcedFail, out)
	}
	if opts.Log != nil {
		fmt.Fprintf(opts.Log, "self-check: %s: forced fail failed %s in %s\n", pkgPath, probe.name, time.Since(start).Round(time.Millisecond))
	}
	return nil
}
