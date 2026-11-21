// Command gamut runs mutation testing on Go packages.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/ambi/gamut/internal/accept"
	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/config"
	"github.com/ambi/gamut/internal/diff"
	"github.com/ambi/gamut/internal/equiv"
	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/report"
	"github.com/ambi/gamut/internal/run"
)

func main() {
	os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr))
}

// Exit codes.
const (
	exitOK = 0
	// exitError is an error in the arguments, the configuration, loading, building, and so on.
	exitError = 1
	// exitQuality means judging finished but the quality gate (critical regions, thresholds, -fail-on-survived) failed.
	exitQuality = 2
	// exitBaseline means tests failed without mutations, so nothing was judged.
	exitBaseline = 4
	// exitSelfCheck means the forced fail self-check found that mutations do not reach the tests, so nothing was judged.
	exitSelfCheck = 5
)

func realMain(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "accept" {
		return acceptMain(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "merge" {
		return mergeMain(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "select-tests" {
		return judge(args[1:], stdout, stderr, modeSelectTests)
	}
	return judge(args, stdout, stderr, modeJudge)
}

// mode is how judge behaves.
type mode int

const (
	// modeJudge judges mutants.
	modeJudge mode = iota
	// modePrune removes acceptances no longer needed from the record after judging (gamut accept -prune).
	modePrune
	// modeSelectTests runs nothing and prints the tests the diff may break, selected from the cache (gamut select-tests).
	modeSelectTests
)

// mergeMain runs gamut merge: it combines the caches of shards run with -shard into one report and one cache.
// The report is combined from the shard runs' reports kept in each cache; report files are not read.
// It writes the combined report's summary to stdout and returns exit code 2 when the quality gate fails.
func mergeMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gamut merge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "write the merged report (mutation-testing-elements JSON) to this file; it embeds the sources, read from the shards' module directory or else the module of the working directory")
	cacheOut := fs.String("cache-out", "", "also write the merged cache to this file")
	failOnSurvived := fs.Bool("fail-on-survived", false, "exit 2 when any unaccepted mutant survives")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: gamut merge [-o <report>] [-cache-out <cache>] <shard cache>...")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return exitError
	}
	// Combine the reports of the shard runs kept in each shard's cache.
	var reps []*report.Report
	for _, path := range fs.Args() {
		data, err := cache.ReadLast(path)
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return exitError
		}
		var r report.Report
		if err := json.Unmarshal(data, &r); err != nil {
			fmt.Fprintf(stderr, "gamut: read the report in the cache %s: %v\n", path, err)
			return exitError
		}
		reps = append(reps, &r)
	}
	rep, err := report.Merge(reps)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return exitError
	}
	if *output != "" {
		// The report embeds the original sources: from the module directory the shards judged if it exists here, otherwise from the working directory's module.
		dir := rep.ModuleDir
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); dir == "" || err != nil {
			if dir, err = moduleRoot(); err != nil {
				fmt.Fprintln(stderr, "gamut:", err)
				return exitError
			}
		}
		mte, err := rep.MTE(dir)
		if err == nil {
			err = writeMTE(*output, mte)
		}
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return exitError
		}
	}
	if *cacheOut != "" {
		if err := cache.Merge(fs.Args(), *cacheOut); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return exitError
		}
	}
	report.WriteSummary(stdout, rep)
	return qualityCode(rep, *failOnSurvived, stderr)
}

// acceptMain runs gamut accept.
// Given identifiers, it adds them to the acceptance record at the module root. With -prune, it judges with the remaining arguments and removes acceptances no longer needed.
func acceptMain(args []string, stdout, stderr io.Writer) int {
	// What follows -prune are judging flags and patterns, so they are not parsed as accept flags.
	if len(args) > 0 && (args[0] == "-prune" || args[0] == "--prune") {
		return judge(args[1:], stdout, stderr, modePrune)
	}
	fs := flag.NewFlagSet("gamut accept", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reason := fs.String("reason", "", "why the mutants are equivalent (required unless -prune)")
	dir := fs.String("dir", ".", "a directory inside the module whose "+accept.FileName+" to update")
	// -prune is accepted only first; it is defined here to appear in the usage.
	prune := fs.Bool("prune", false, "as the first argument: judge the packages given as the remaining arguments (with any gamut flags) and remove acceptances that a test now detects or whose mutant is gone")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: gamut accept -reason <text> [-dir <dir>] <mutant-id>...")
		fmt.Fprintln(stderr, "       gamut accept -prune [gamut flags] <package-directory>[/...] ...")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	if *prune {
		fmt.Fprintln(stderr, "gamut: -prune must be the first argument of gamut accept")
		return exitError
	}
	if fs.NArg() == 0 || *reason == "" {
		fs.Usage()
		return exitError
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 1
	}
	root := load.ModuleRoot(abs)
	if root == "" {
		fmt.Fprintf(stderr, "gamut: %s is not inside a Go module\n", abs)
		return 1
	}
	f, err := accept.Load(root)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 1
	}
	if err := f.Add(fs.Args(), *reason); err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return exitError
	}
	if err := f.Save(); err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 1
	}
	fmt.Fprintf(stdout, "accepted %d mutants in %s\n", fs.NArg(), f.Path())
	return 0
}

// judge judges the mutants of the packages, behaving as m says.
func judge(args []string, stdout, stderr io.Writer, m mode) int {
	prune := m == modePrune
	fs := flag.NewFlagSet("gamut", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var selectFormat *string
	if m == modeSelectTests {
		selectFormat = fs.String("format", "run", "output format: run (a go test -run pattern per package) or json (groups, counts, and reasons)")
	}
	workers := fs.Int("workers", max(1, runtime.NumCPU()/2), "number of parallel test processes")
	only := fs.String("only", "", "comma-separated mutator types to run, opt-in ones included (default: the default set; see -list-mutators)")
	disable := fs.String("disable", "", "comma-separated mutator types to remove from the default set; cannot be combined with -only")
	listMutators := fs.Bool("list-mutators", false, "print every mutator type with its description, example, and whether it is in the default set, then exit")
	showVersion := fs.Bool("version", false, "print the gamut version and exit")
	output := fs.String("o", "", "write the JSON report (mutation-testing-elements) to this path")
	dryRun := fs.Bool("dry-run", false, "list mutants and their viability without running tests")
	timeoutFactor := fs.Float64("timeout-factor", 5, "multiply the selected tests' baseline time by this for the per-mutant timeout")
	timeoutMin := fs.Duration("timeout-min", 2*time.Second, "add this to every per-mutant timeout")
	quiet := fs.Bool("q", false, "print only the summary")
	tags := fs.String("tags", "", "comma-separated build tags for loading, building, and type-checking")
	testFlags := fs.String("test-flags", "", "space-separated flags for every test binary run, such as \"-short\"")
	cachePath := fs.String("cache", filepath.Join(".gamut", "cache.json"), "read and write reusable verdicts at this path")
	noCache := fs.Bool("no-cache", false, "neither reuse nor record verdicts and coverage")
	checkpoint := fs.Duration("checkpoint-interval", 10*time.Second, "also write the cache this often during the run, so that a hard kill loses at most this much progress (0 disables)")
	since := fs.String("since", "", "judge only mutants intersecting the diff from the merge-base of this git ref to the working tree; report the rest as PENDING")
	scope := fs.String("scope", "node", "with -since, intersect the diff with each mutant's expression (node) or its enclosing declaration (decl)")
	includeTestChanges := fs.Bool("include-test-changes", false, "with -since, also recheck survivors outside the diff that changed tests reach")
	var ids multiFlag
	fs.Var(&ids, "mutant", "judge only the mutant with this ID or unique ID prefix, ignoring its cached verdict (repeatable)")
	isolate := fs.Bool("isolate", false, "start a new test process for every mutant instead of reusing test processes during the run")
	cpuProfile := fs.String("cpuprofile", "", "write a CPU profile of gamut itself to this path (for developing gamut)")
	integration := fs.Bool("integration", false, "also try to detect mutants with the tests of module packages that import the target packages")
	detectEquivalent := fs.Bool("detect-equivalent", false, "compile each surviving mutant and report it as EQUIVALENT when its machine code equals the original's (one package compile per survivor, outside the build cache)")
	var critical multiFlag
	fs.Var(&critical, "critical", "treat files whose module-relative path matches this glob (** for any directories) as a critical region, where every mutant must be detected or accepted (repeatable)")
	thresholdHigh := fs.Float64("threshold-high", 80, "label a mutation score at or above this percentage high")
	thresholdLow := fs.Float64("threshold-low", 60, "label a mutation score at or above this percentage low, and below it danger")
	thresholdBreak := fs.Float64("threshold-break", 0, "exit with code 2 when the mutation score is below this percentage (0 disables)")
	failOnSurvived := fs.Bool("fail-on-survived", false, "exit with code 2 when any unaccepted mutant survives")
	var reporterKinds multiFlag
	fs.Var(&reporterKinds, "reporter", "report format: text (the summary on stdout), json (mutation-testing-elements), html, agent (one JSON document on stdout for coding agents), or simplify (surviving removals by function, as candidates for unneeded code) (repeatable; default text, plus json with -o)")
	reportDir := fs.String("report-dir", filepath.Join(".gamut", "report"), "write the html report, and the json report without -o, to this directory")
	annotations := fs.String("annotations", "", "also print the survivors to fix as CI annotations on stdout (values: github)")
	watch := fs.Bool("watch", false, "keep running: judge the diff from HEAD (with -include-test-changes) again each time .go files in the module are saved; failures do not stop the watch")
	shardSpec := fs.String("shard", "", "judge only shard k/n of the mutants, split by a hash of their IDs; combine the shards' reports and caches with gamut merge")
	timeBudget := fs.Duration("time-budget", 0, "stop starting mutants once the run has taken this long and report the rest as PENDING; the next run with the same cache continues (0: no budget)")
	killMatrix := fs.Bool("kill-matrix", false, "run every infected test of each mutant in a fresh process instead of stopping at the first failure, record all tests that detect it, and report tests that detect nothing, subsumed tests, and duplicate tests")
	maxDepth := fs.Int("max-depth", 0, "count a test as reaching a mutated site only within this many calls of module functions from the test function (0: no limit)")
	var aridCalls, aridPackages multiFlag
	fs.Var(&aridCalls, "arid-call", "do not mutate the arguments of, or remove, calls matching this glob on <package>.<Func> or <package>.<Type>.<Method> (repeatable)")
	fs.Var(&aridPackages, "arid-package", "do not mutate the arguments of, or remove, calls into this package or its subpackages (repeatable)")
	noArid := fs.Bool("no-arid", false, "mutate logging, metrics, and other code that tests rarely observe (arid code), which is skipped by default")
	var excludes multiFlag
	fs.Var(&excludes, "exclude-files", "do not mutate files whose module-relative path matches this regular expression (repeatable)")
	configPath := fs.String("config", "", "read settings from this JSON file instead of "+config.FileName+" at the module root; flags override it")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: gamut [flags] <package-directory>[/...] ...")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	if *showVersion {
		fmt.Fprintln(stdout, "gamut", version())
		return exitOK
	}
	if *listMutators {
		writeMutators(stdout)
		return exitOK
	}
	if *watch {
		if m != modeJudge {
			fmt.Fprintln(stderr, "gamut: -watch is for judging only")
			return exitError
		}
		var inner []string
		for _, a := range args {
			if a != "-watch" && a != "--watch" && !strings.HasPrefix(a, "-watch=") && !strings.HasPrefix(a, "--watch=") {
				inner = append(inner, a)
			}
		}
		// The default range is the diff from HEAD plus the survivors changed tests reach.
		if *since == "" {
			inner = append([]string{"-since", "HEAD"}, inner...)
			if !*includeTestChanges {
				inner = append([]string{"-include-test-changes"}, inner...)
			}
		}
		return watchLoop(inner, fs.Args(), stdout, stderr)
	}
	if m == modeSelectTests {
		switch {
		case *since == "":
			fmt.Fprintln(stderr, "gamut: select-tests needs -since <ref>")
			return exitError
		case *noCache:
			fmt.Fprintln(stderr, "gamut: select-tests reads the cache; it cannot be combined with -no-cache")
			return exitError
		case *selectFormat != "run" && *selectFormat != "json":
			fmt.Fprintf(stderr, "gamut: unknown -format %q; use run or json\n", *selectFormat)
			return exitError
		}
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return exitError
	}
	abs, err := load.AbsPatterns(fs.Args())
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 1
	}
	earlyDir := load.ModuleRoot(strings.TrimSuffix(abs[0], string(filepath.Separator)+"..."))
	// Config file values go only into flags not given on the command line.
	cfg, err := readConfig(*configPath, earlyDir)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 1
	}
	if cfg != nil {
		if err := applyConfig(fs, cfg); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
	}
	excludeFiles, err := compileExcludes(excludes)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return exitError
	}
	types, err := selectTypes(*only, *disable)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return exitError
	}
	flags, err := run.ParseTestFlags(*testFlags)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return exitError
	}
	outputs, err := newReporters(reporterKinds, *output, *reportDir, *annotations)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return exitError
	}
	thresholds := report.Thresholds{High: *thresholdHigh, Low: *thresholdLow, Break: *thresholdBreak}
	if msg := checkThresholds(thresholds); msg != "" {
		fmt.Fprintln(stderr, "gamut:", msg)
		return exitError
	}
	if msg := checkSelectionFlags(*since, *scope, *includeTestChanges, ids); msg != "" {
		fmt.Fprintln(stderr, "gamut:", msg)
		return exitError
	}

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
		defer func() { _ = f.Close() }()
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
		defer pprof.StopCPUProfile()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	start := time.Now()
	logw := stderr
	if *quiet {
		logw = io.Discard
	}
	if cfg != nil {
		fmt.Fprintf(logw, "config: %s\n", cfg.Path)
	}

	settings := []string{
		"types=" + strings.Join(slices.Sorted(slices.Values(typeNames(types))), ","),
		"tags=" + *tags,
		"test-flags=" + strings.Join(flags, " "),
		fmt.Sprintf("timeout=%g,%s", *timeoutFactor, *timeoutMin),
	}
	// The depth limit changes reach records, so it is part of the cache environment. Without it nothing is added, keeping existing caches.
	if *maxDepth > 0 {
		settings = append(settings, fmt.Sprintf("max-depth=%d", *maxDepth))
	}
	// A run without selection returns the previous report, without loading or building, when the inputs match the previous run.
	// When a budget left mutants pending, the previous report lacks the rest, so the shortcut is not used.
	quickable := !*noCache && !*dryRun && len(ids) == 0 && *since == "" && m == modeJudge && *timeBudget == 0
	var early *cache.Store
	if !*noCache && earlyDir != "" {
		if early, err = openStore(*cachePath, earlyDir, settings); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
	}
	var quickKey string
	if quickable {
		// Every flag that changes the report goes into the key. -fail-on-survived does not change the report and is applied to the replayed one.
		quickKey = cache.QuickKey(slices.Concat(abs, settings, []string{
			fmt.Sprintf("integration=%t", *integration), fmt.Sprintf("detect-equivalent=%t", *detectEquivalent),
			"exclude=" + strings.Join(excludes, "\x00"), "critical=" + strings.Join(critical, "\x00"),
			fmt.Sprintf("arid=%t,%s,%s", *noArid, strings.Join(aridCalls, "\x00"), strings.Join(aridPackages, "\x00")),
			fmt.Sprintf("kill-matrix=%t", *killMatrix), "shard=" + *shardSpec,
			fmt.Sprintf("thresholds=%g,%g,%g", thresholds.High, thresholds.Low, thresholds.Break),
		})...)
		if early != nil {
			if rep, ok := replay(early, quickKey, start); ok {
				fmt.Fprintln(logw, "cache: no input changed since the last run; reporting its verdicts")
				if err := outputs.deliver(rep, earlyDir, stdout); err != nil {
					fmt.Fprintln(stderr, "gamut:", err)
					return exitError
				}
				return qualityCode(rep, *failOnSurvived, stderr)
			}
		}
	}
	// git diff and the dependency list start without waiting for loading. If the module differs from what loading finds, they are discarded and recomputed.
	var pre prefetch
	if earlyDir != "" && !*dryRun {
		pre.start(earlyDir, splitTags(*tags), abs, *since, !*noCache || *since != "")
	}

	// Request the packages whose export data the previous load requested, alongside the dependency list.
	loadKey := cache.QuickKey(slices.Concat([]string{"load", *tags}, abs)...)
	var hint []string
	if early != nil {
		hint = early.LoadHint(loadKey)
	}
	pkgs, hint, skippedPkgs, err := load.WithHint(fs.Args(), splitTags(*tags), hint)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 1
	}
	for _, s := range skippedPkgs {
		fmt.Fprintf(stderr, "gamut: warning: skipping %s: %s\n", s.ImportPath, s.Reason)
	}
	targets := make([]run.Target, len(pkgs))
	paths := make([]string, len(pkgs))
	total := 0
	accepted, err := accept.Load(pkgs[0].ModuleDir)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 1
	}
	var ignored []mutant.Ignored
	criticalIDs := map[string]bool{}
	aridRules := &mutant.Arid{Off: *noArid, Calls: aridCalls, Packages: aridPackages}
	aridSkipped := 0
	for i, pkg := range pkgs {
		enumerated, aridN, err := mutant.EnumerateWith(pkg, types, aridRules)
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return exitError
		}
		aridSkipped += aridN
		mutants := mutant.Without(enumerated, func(m *mutant.Mutant) bool { return excluded(excludeFiles, m.File.Rel) })
		mutants, skipped, warnings, err := mutant.Ignores(pkg, mutants)
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
		crit, critWarnings, err := mutant.Critical(pkg, mutants, critical)
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return exitError
		}
		maps.Copy(criticalIDs, crit)
		for _, w := range slices.Concat(warnings, critWarnings) {
			fmt.Fprintln(stderr, "gamut: warning:", w)
		}
		ignored = append(ignored, skipped...)
		targets[i] = run.Target{Pkg: pkg, Mutants: mutants}
		paths[i] = pkg.ImportPath
		total += len(mutants)
		if len(skipped) > 0 {
			fmt.Fprintf(logw, "load: %s, %d mutants, %d ignored by comments\n", pkg.ImportPath, len(mutants), len(skipped))
		} else {
			fmt.Fprintf(logw, "load: %s, %d mutants\n", pkg.ImportPath, len(mutants))
		}
	}
	fmt.Fprintf(logw, "load: %d packages, %d mutants in %s\n", len(pkgs), total, time.Since(start).Round(time.Millisecond))
	if *dryRun {
		checkViability(targets, nil)
		for _, t := range targets {
			for _, m := range t.Mutants {
				fmt.Fprintf(stdout, "%s:%d:%d\t%s\t%s->%s\tviable=%t\t%s\n", m.File.Rel, m.Line, m.Column, m.Type, m.OriginalText, m.ReplacementText, m.Viable, m.ID)
			}
		}
		return 0
	}

	opts := run.Options{
		Workers: *workers, TimeoutFactor: *timeoutFactor, TimeoutMin: *timeoutMin, Log: logw, Warn: stderr, TestFlags: flags,
		Isolate: *isolate,
	}
	if *maxDepth < 0 {
		fmt.Fprintln(stderr, "gamut: -max-depth must not be negative")
		return exitError
	}
	opts.MaxDepth = *maxDepth
	opts.KillMatrix = *killMatrix
	if *shardSpec != "" {
		if opts.Shard, err = run.ParseShard(*shardSpec); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return exitError
		}
	}
	if *timeBudget > 0 {
		opts.Deadline = start.Add(*timeBudget)
	}
	var unrepeatableMu sync.Mutex
	var unrepeatable []string
	depthTests, depthReaches := 0, 0
	opts.DepthExcluded = func(_ string, tests, reaches int) {
		unrepeatableMu.Lock()
		defer unrepeatableMu.Unlock()
		depthTests += tests
		depthReaches += reaches
	}
	opts.Unrepeatable = func(pkg string, tests []string) {
		unrepeatableMu.Lock()
		defer unrepeatableMu.Unlock()
		for _, name := range tests {
			// When judging several packages, test names say which package's tests they are.
			if len(targets) > 1 {
				name = pkg + "." + name
			}
			unrepeatable = append(unrepeatable, name)
		}
	}
	var changes *diff.Changes
	switch {
	case len(ids) > 0:
		selected, err := resolveMutants(ids, targets, ignored)
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
		opts.Select = &run.Selection{Mutants: selected}
	case *since != "":
		changes, err = pre.changes(pkgs[0].ModuleDir, *since)
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
		fmt.Fprintf(logw, "since: %d changed files since %.12s\n", changes.Files(), changes.Base)
		opts.Select = &run.Selection{Changes: changes, DeclScope: *scope == "decl", IncludeTestChanges: *includeTestChanges}
	}
	var revdeps map[string][]load.RevDep
	if *integration {
		if revdeps, err = load.ReverseDeps(pkgs); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
		for i := range targets {
			for _, r := range revdeps[targets[i].Pkg.ImportPath] {
				targets[i].RevDeps = append(targets[i].RevDeps, run.RevDep{RevDep: r})
			}
			fmt.Fprintf(logw, "integration: %s: %d reverse dependencies with tests\n", targets[i].Pkg.ImportPath, len(targets[i].RevDeps))
		}
	}
	// Keys also serve to find the tests the diff changed, so -since computes them even without the cache.
	var sources map[string][]string
	if !*noCache || changes != nil {
		if sources, err = computeKeys(pkgs, targets, !*noCache, &pre); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
	}
	var quick *cache.Quick
	if quickable {
		// The input state is taken before running tests, so files changed during the run are not recorded in their earlier state.
		if quick, err = quickInputs(fs.Args(), targets, sources, accepted.Path()); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
	}
	if m == modeSelectTests {
		// The cache is only read, and records of removed mutants are kept, so verdicts from before a declaration was rewritten can be looked up by declaration name.
		store, err := openStore(*cachePath, pkgs[0].ModuleDir, settings)
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return exitError
		}
		return selectTestsMain(stdout, stderr, *selectFormat, targets, changes, store)
	}
	if !*noCache {
		if early != nil && earlyDir != pkgs[0].ModuleDir {
			early = nil
		}
		store, err := openCache(*cachePath, settings, pkgs[0].ModuleDir, targets, changes, early)
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
		if store.Reset != "" {
			fmt.Fprintf(logw, "cache: discarding previous verdicts because %s\n", store.Reset)
		}
		opts.Cache = store
		store.PutLoadHint(loadKey, hint)
		// Even when stopped by cancellation or failing baseline tests, keep the verdicts run so far.
		defer func() {
			if err := store.Save(); err != nil {
				fmt.Fprintln(stderr, "gamut: warning: saving the cache:", err)
			}
		}()
		// A SIGKILL (out of memory, a CI timeout) skips the write above, so the cache is also written at an interval.
		// Deferred calls run last-registered first, so checkpointing stops before the final write.
		if *checkpoint > 0 {
			defer startCheckpoint(store, *checkpoint, stderr)()
		}
	}

	vstart := time.Now()
	checked := checkViability(targets, opts.Cache)
	fmt.Fprintf(logw, "viability: %d mutants type-checked, %d reused from the cache in %s\n", checked, total-checked, time.Since(vstart).Round(time.Millisecond))

	perPackage, err := run.All(ctx, targets, opts)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return exitCode(err)
	}
	if *detectEquivalent {
		work, err := os.MkdirTemp("", "gamut-equiv-")
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
		estart := time.Now()
		stats, err := equiv.Mark(ctx, targets, perPackage, opts.Cache, *workers, work)
		_ = os.RemoveAll(work)
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
		for _, p := range stats.Skipped {
			fmt.Fprintf(stderr, "gamut: warning: %s: not checked for equivalent mutants: %s\n", p, equiv.ErrUnsupported)
		}
		fmt.Fprintf(logw, "equivalent: %d survivors compiled, %d reused from the cache, %d equivalent in %s\n",
			stats.Checked, stats.Reused, stats.Equivalent, time.Since(estart).Round(time.Millisecond))
	}
	var results []*run.Result
	for _, rs := range perPackage {
		for _, r := range rs {
			// When selected by identifier, only the selected mutants are reported.
			if opts.Select != nil && opts.Select.Mutants != nil && r.Status == run.Pending {
				continue
			}
			results = append(results, r)
		}
	}
	label := paths[0]
	if len(pkgs) > 1 {
		label = strings.Join(fs.Args(), " ")
	}
	rep := report.Build(pkgs[0].ModulePath, label, paths, results, time.Since(start))
	rep.Version, rep.ModuleDir = version(), pkgs[0].ModuleDir
	rep.AddIgnored(ignored)
	rep.MutantsArid = aridSkipped
	rep.UnrepeatableTests = slices.Sorted(slices.Values(unrepeatable))
	rep.MaxDepthExcludedTests, rep.MaxDepthExcludedReaches = depthTests, depthReaches
	if *killMatrix {
		rep.ComputeKillMatrix()
	}
	for _, s := range skippedPkgs {
		rep.SkippedPackages = append(rep.SkippedPackages, report.SkippedPackage{Package: s.ImportPath, Reason: s.Reason})
	}
	rep.MarkCritical(criticalIDs)
	rep.Thresholds = &thresholds
	// When selected by identifier, other mutants are not reported, so acceptances cannot be judged "missing".
	var acceptedHere []string
	if opts.Select == nil || opts.Select.Mutants == nil {
		acceptedHere = acceptedInPackages(accepted, pkgs)
	}
	rep.ApplyAccepted(acceptedHere, accepted.Reason)
	if prune {
		var stale []string
		for _, s := range rep.StaleAccepted {
			stale = append(stale, s.ID)
		}
		accepted.Remove(stale)
		if err := accepted.Save(); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
		fmt.Fprintf(logw, "accept: removed %d stale acceptances from %s\n", len(stale), accepted.Path())
		rep.StaleAccepted = nil
	}
	if err := outputs.deliver(rep, pkgs[0].ModuleDir, stdout); err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return exitError
	}
	if opts.Cache != nil {
		var buf strings.Builder
		if err := rep.WriteJSON(&buf); err == nil {
			// gamut merge reads the report of the last run to combine the shards' verdicts.
			opts.Cache.PutLast(json.RawMessage(buf.String()))
			if quick != nil {
				quick.Report = json.RawMessage(buf.String())
				opts.Cache.PutQuick(quickKey, quick)
			}
		}
	}
	return qualityCode(rep, *failOnSurvived, stderr)
}

// qualityCode checks whether the finished report passes the quality gate; if not, it writes why and returns exitQuality.
func qualityCode(rep *report.Report, failOnSurvived bool, stderr io.Writer) int {
	failures := rep.Quality(failOnSurvived)
	if len(failures) == 0 {
		return exitOK
	}
	fmt.Fprintln(stderr, "gamut: quality gate failed: "+strings.Join(failures, "; "))
	return exitQuality
}

// checkThresholds checks the combination of thresholds and returns an explanation of any error.
func checkThresholds(t report.Thresholds) string {
	for _, v := range []float64{t.High, t.Low, t.Break} {
		if v < 0 || v > 100 {
			return "thresholds must be between 0 and 100"
		}
	}
	if t.Low > t.High {
		return "-threshold-low must not exceed -threshold-high"
	}
	return ""
}

// startCheckpoint starts writing the cache every interval. The returned function stops it, waiting for a write in progress.
func startCheckpoint(store *cache.Store, interval time.Duration, stderr io.Writer) func() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := store.Save(); err != nil {
					fmt.Fprintln(stderr, "gamut: warning: writing a cache checkpoint:", err)
				}
			}
		}
	})
	return func() {
		close(done)
		wg.Wait()
	}
}

// openStore opens the cache under the environment of moduleDir.
func openStore(path, moduleDir string, settings []string) (*cache.Store, error) {
	env, err := cache.Environment(moduleDir, settings...)
	if err != nil {
		return nil, err
	}
	return cache.Open(path, env)
}

// replay returns the report of the previous run of key when its inputs are unchanged. The second result reports whether it could.
func replay(store *cache.Store, key string, start time.Time) (*report.Report, bool) {
	q := store.Quick(key)
	if q == nil {
		return nil, false
	}
	fp, err := cache.Fingerprint(q.Files, q.Dirs, q.Trees)
	if err != nil || fp != q.Fingerprint {
		return nil, false
	}
	var rep report.Report
	if err := json.Unmarshal(q.Report, &rep); err != nil {
		return nil, false
	}
	rep.MarkReused()
	rep.ElapsedTime = time.Since(start).Seconds()
	return &rep, true
}

// Report formats.
const (
	reporterText     = "text"
	reporterJSON     = "json"
	reporterHTML     = "html"
	reporterAgent    = "agent"
	reporterSimplify = "simplify"
)

// reporters says how reports are produced.
type reporters struct {
	kinds []string
	// output is where -o writes, in json (mutation-testing-elements) format. dir is -report-dir,
	// where json without -o and html go.
	output, dir string
	// annotations is the CI annotation format (github), or empty for none.
	annotations string
}

// newReporters builds reporters from the -reporter values, text by default.
// With -o, json goes there regardless of -reporter.
func newReporters(kinds []string, output, dir, annotations string) (*reporters, error) {
	if annotations != "" && annotations != "github" {
		return nil, fmt.Errorf("unknown -annotations %q; use github", annotations)
	}
	if len(kinds) == 0 {
		kinds = []string{reporterText}
	}
	for _, k := range kinds {
		if !slices.Contains([]string{reporterText, reporterJSON, reporterHTML, reporterAgent, reporterSimplify}, k) {
			return nil, fmt.Errorf("unknown -reporter %q; use text, json, html, agent, or simplify", k)
		}
	}
	// agent writes exactly one JSON document to stdout, so it does not mix with other stdout formats.
	if slices.Contains(kinds, reporterAgent) && (slices.Contains(kinds, reporterText) || slices.Contains(kinds, reporterSimplify) || annotations != "") {
		return nil, errors.New("-reporter agent writes one JSON document to stdout; it cannot be combined with -reporter text, -reporter simplify, or -annotations")
	}
	if output != "" && !slices.Contains(kinds, reporterJSON) {
		kinds = append(kinds, reporterJSON)
	}
	return &reporters{kinds: kinds, output: output, dir: dir, annotations: annotations}, nil
}

// repoPrefix returns the path from the Git repository root to moduleDir, or empty outside a repository.
// CI annotation files are relative to the repository, so it goes before module-relative paths.
func repoPrefix(moduleDir string) string {
	out, err := exec.Command("git", "-C", moduleDir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		return ""
	}
	dir, err := filepath.EvalSymlinks(moduleDir)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(top, dir)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

// deliver writes the report in the given formats. mutation-testing-elements formats (json, html) read file contents from moduleDir.
func (o *reporters) deliver(rep *report.Report, moduleDir string, stdout io.Writer) error {
	var mte *report.MTEReport
	for _, k := range o.kinds {
		var err error
		switch k {
		case reporterText:
			report.WriteSummary(stdout, rep)
		case reporterSimplify:
			report.WriteSimplify(stdout, rep)
		case reporterAgent:
			out, aerr := rep.Agent(moduleDir)
			if aerr != nil {
				return aerr
			}
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			err = enc.Encode(out)
		case reporterJSON, reporterHTML:
			if mte == nil {
				if mte, err = rep.MTE(moduleDir); err != nil {
					return err
				}
			}
			if k == reporterJSON {
				path := o.output
				if path == "" {
					path = filepath.Join(o.dir, "mutation.json")
				}
				err = writeMTE(path, mte)
			} else {
				err = writeFile(filepath.Join(o.dir, "mutation.html"), func(w io.Writer) error { return report.WriteHTML(w, mte) })
			}
		}
		if err != nil {
			return err
		}
	}
	if o.annotations == "github" {
		report.WriteGitHubAnnotations(stdout, rep, repoPrefix(moduleDir))
	}
	return nil
}

// quickInputs gathers the input files and directories the next run checks to decide whether the previous report can be returned as is, and fingerprints their current state:
// the files directly in the directories of the target packages and dependency files, the dependency files, and the trees under the roots of <directory>/... patterns.
// go.mod, go.sum, go.work, gamut, and the Go version are covered by the cache's environment key.
// extra are other files whose state is compared (such as the acceptance record).
func quickInputs(patterns []string, targets []run.Target, sources map[string][]string, extra ...string) (*cache.Quick, error) {
	files := map[string]bool{}
	for _, f := range extra {
		files[f] = true
	}
	dirs := map[string]bool{}
	for _, t := range targets {
		dirs[t.Pkg.Dir] = true
		for _, f := range sources[t.Pkg.ImportPath] {
			files[f] = true
			dirs[filepath.Dir(f)] = true
		}
		for _, r := range t.RevDeps {
			dirs[r.Dir] = true
			for _, f := range r.Files {
				files[f] = true
				dirs[filepath.Dir(f)] = true
			}
		}
	}
	abs, err := load.AbsPatterns(patterns)
	if err != nil {
		return nil, err
	}
	var trees []string
	for d := range dirs {
		if info, err := os.Stat(filepath.Join(d, "testdata")); err == nil && info.IsDir() {
			trees = append(trees, filepath.Join(d, "testdata"))
		}
	}
	for _, p := range abs {
		if root, ok := strings.CutSuffix(p, string(filepath.Separator)+"..."); ok {
			trees = append(trees, root)
		}
	}
	slices.Sort(trees)
	q := &cache.Quick{Files: slices.Sorted(maps.Keys(files)), Dirs: slices.Sorted(maps.Keys(dirs)), Trees: trees}
	if q.Fingerprint, err = cache.Fingerprint(q.Files, q.Dirs, q.Trees); err != nil {
		return nil, err
	}
	return q, nil
}

// checkViability checks the viability of mutants. With store, mutants with unchanged keys use their records, and verdicts are recorded.
// It returns the number of mutants type-checked.
func checkViability(targets []run.Target, store *cache.Store) int {
	checked := 0
	for _, t := range targets {
		var known func(*mutant.Mutant) (bool, bool)
		if store != nil && t.Keys != nil {
			known = func(m *mutant.Mutant) (bool, bool) { return store.Viable(m.ID, t.Keys.Viability(m)) }
		}
		before := map[*mutant.Mutant]bool{}
		if known != nil {
			for _, m := range t.Mutants {
				if _, ok := known(m); ok {
					before[m] = true
				}
			}
		}
		mutant.CheckViabilityWith(t.Pkg, t.Mutants, known)
		checked += len(t.Mutants) - len(before)
		if known == nil {
			continue
		}
		for _, m := range t.Mutants {
			if !before[m] {
				store.PutViable(m.ID, &cache.Viability{Package: t.Pkg.ImportPath, Key: t.Keys.Viability(m), Viable: m.Viable})
			}
		}
	}
	return checked
}

// computeKeys attaches reuse keys to targets, and with withRevDeps also the test keys of reverse dependencies.
// It returns the files of the external dependencies per package.
func computeKeys(pkgs []*load.Package, targets []run.Target, withRevDeps bool, pre *prefetch) (map[string][]string, error) {
	sources, err := pre.sources(pkgs)
	if err != nil {
		return nil, err
	}
	for i := range targets {
		pkg := targets[i].Pkg
		if targets[i].Keys, err = cache.NewKeys(pkg, sources[pkg.ImportPath]); err != nil {
			return nil, err
		}
		if !withRevDeps {
			continue
		}
		for k := range targets[i].RevDeps {
			r := &targets[i].RevDeps[k]
			if r.Key, err = cache.RevDepKey(pkg.ModuleDir, r.Files); err != nil {
				return nil, err
			}
		}
	}
	return sources, nil
}

// openCache reads the cache, carries previous verdicts over to mutants of renamed files, and discards records of removed mutants.
// When opened is not nil, it is used as opened under the same module environment.
func openCache(path string, settings []string, moduleDir string, targets []run.Target, changes *diff.Changes, opened *cache.Store) (*cache.Store, error) {
	store := opened
	if store == nil {
		var err error
		if store, err = openStore(path, moduleDir, settings); err != nil {
			return nil, err
		}
	}
	live := map[string]bool{}
	paths := make([]string, len(targets))
	for i := range targets {
		paths[i] = targets[i].Pkg.ImportPath
		for _, m := range targets[i].Mutants {
			live[m.ID] = true
			if changes == nil {
				continue
			}
			if old, ok := changes.Renames[resolvePath(m.File.Path)]; ok {
				if rel, err := filepath.Rel(resolvePath(moduleDir), old); err == nil {
					store.Inherit(m.IDAt(filepath.ToSlash(rel)), m.ID)
				}
			}
		}
	}
	store.Prune(paths, func(id string) bool { return live[id] })
	return store, nil
}

// resolvePath returns the path with symbolic links resolved, or the path as is when it cannot.
func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// resolveMutants finds mutants from identifiers or unique identifier prefixes.
// A value that matches nothing or several mutants gives an error listing the candidates.
func resolveMutants(ids []string, targets []run.Target, ignored []mutant.Ignored) (map[*mutant.Mutant]bool, error) {
	out := map[*mutant.Mutant]bool{}
	for _, id := range ids {
		var hits, exact []*mutant.Mutant
		for _, t := range targets {
			for _, m := range t.Mutants {
				if m.ID == id {
					exact = append(exact, m)
				}
				if strings.HasPrefix(m.ID, id) {
					hits = append(hits, m)
				}
			}
		}
		if len(exact) > 0 {
			hits = exact
		}
		switch len(hits) {
		case 0:
			for _, ig := range ignored {
				if strings.HasPrefix(ig.Mutant.ID, id) {
					return nil, fmt.Errorf("%q matches %s, which a //gamut:ignore comment covers at %s:%d", id, ig.Mutant.ID, ig.Mutant.File.Rel, ig.Mutant.Line)
				}
			}
			return nil, fmt.Errorf("no mutant matches %q", id)
		case 1:
			out[hits[0]] = true
		default:
			var lines []string
			for _, m := range hits {
				lines = append(lines, fmt.Sprintf("  %s (%s:%d:%d)", m.ID, m.File.Rel, m.Line, m.Column))
			}
			return nil, fmt.Errorf("%q matches %d mutants; give a longer prefix:\n%s", id, len(hits), strings.Join(lines, "\n"))
		}
	}
	return out, nil
}

// checkSelectionFlags checks the combination of selection flags and returns an explanation of any error.
// acceptedInPackages returns the identifiers of acceptances pointing at files of the packages being judged.
func acceptedInPackages(f *accept.File, pkgs []*load.Package) []string {
	var dirs []string
	for _, p := range pkgs {
		rel, err := filepath.Rel(p.ModuleDir, p.Dir)
		if err != nil {
			continue
		}
		dirs = append(dirs, filepath.ToSlash(rel))
	}
	var out []string
	for _, id := range f.IDs() {
		if accept.InPackages(id, dirs) {
			out = append(out, id)
		}
	}
	return out
}

// readConfig reads the file at path if given, otherwise the config file of moduleDir, and returns nil when there is neither.
func readConfig(path, moduleDir string) (*config.Config, error) {
	switch {
	case path != "":
		return config.Load(path)
	case moduleDir != "":
		return config.Find(moduleDir)
	}
	return nil, nil
}

// applyConfig puts the config values into the flags not given on the command line.
func applyConfig(fs *flag.FlagSet, cfg *config.Config) error {
	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	for name, values := range cfg.Flags() {
		if explicit[name] {
			continue
		}
		for _, v := range values {
			if err := fs.Set(name, v); err != nil {
				return fmt.Errorf("%s: %s: %w", cfg.Path, name, err)
			}
		}
	}
	return nil
}

// compileExcludes compiles the -exclude-files regular expressions.
func compileExcludes(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("-exclude-files: %w", err)
		}
		out[i] = re
	}
	return out, nil
}

// excluded reports whether the module-relative path rel matches any of the regular expressions.
func excluded(res []*regexp.Regexp, rel string) bool {
	for _, re := range res {
		if re.MatchString(rel) {
			return true
		}
	}
	return false
}

func checkSelectionFlags(since, scope string, includeTestChanges bool, ids []string) string {
	switch {
	case since != "" && len(ids) > 0:
		return "-since and -mutant cannot be combined"
	case scope != "node" && scope != "decl":
		return fmt.Sprintf("-scope must be node or decl, not %q", scope)
	case includeTestChanges && since == "":
		return "-include-test-changes requires -since"
	}
	return ""
}

// multiFlag is a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func typeNames(types []mutant.Type) []string {
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = string(t)
	}
	return out
}

// exitCode maps run errors to exit codes.
func exitCode(err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, run.ErrBaseline):
		return exitBaseline
	case errors.Is(err, run.ErrSelfCheck):
		return exitSelfCheck
	}
	return exitError
}

func splitTags(s string) []string {
	var out []string
	for tag := range strings.SplitSeq(s, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

// selectTypes decides the enabled mutators from -only and -disable, the default set when neither is given.
func selectTypes(only, disable string) ([]mutant.Type, error) {
	if only != "" && disable != "" {
		return nil, errors.New("-only and -disable cannot be combined")
	}
	if disable != "" {
		off, err := parseTypes(disable)
		if err != nil {
			return nil, err
		}
		return slices.DeleteFunc(slices.Clone(mutant.AllTypes), func(t mutant.Type) bool { return slices.Contains(off, t) }), nil
	}
	if only == "" {
		return mutant.AllTypes, nil
	}
	return parseTypes(only)
}

func parseTypes(s string) ([]mutant.Type, error) {
	var out []mutant.Type
	for name := range strings.SplitSeq(s, ",") {
		t := mutant.Type(strings.TrimSpace(name))
		if !slices.Contains(mutant.Known, t) {
			return nil, fmt.Errorf("unknown mutator type %q; see -list-mutators", t)
		}
		out = append(out, t)
	}
	return out, nil
}

// writeMutators writes the list of mutators.
func writeMutators(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, info := range mutant.Infos() {
		set := "opt-in"
		if info.Default {
			set = "default"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", info.Type, set, info.Description, info.Example)
	}
	_ = tw.Flush()
}

// moduleRoot returns the root of the module containing the working directory.
func moduleRoot() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMOD: %w", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" || mod == os.DevNull {
		return "", errors.New("gamut merge -o must run inside the module, whose sources the report embeds")
	}
	return filepath.Dir(mod), nil
}

// writeMTE writes the report in mutation-testing-elements format to path.
func writeMTE(path string, mte *report.MTEReport) error {
	return writeFile(path, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(mte)
	})
}

// writeFile writes what write writes to path, creating the directory when missing.
func writeFile(path string, write func(io.Writer) error) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return write(f)
}

// prefetch holds the git diff and dependency list started alongside loading.
type prefetch struct {
	dir       string
	wg        sync.WaitGroup
	deps      *load.Deps
	depsErr   error
	diff      *diff.Changes
	diffErr   error
	diffStart bool
}

// start starts, in the module dir, git diff when ref is not empty and the dependency list with withDeps.
func (p *prefetch) start(dir string, tags, patterns []string, ref string, withDeps bool) {
	p.dir = dir
	if ref != "" {
		p.diffStart = true
		p.wg.Go(func() { p.diff, p.diffErr = diff.Since(dir, ref) })
	}
	if withDeps {
		p.wg.Go(func() { p.deps, p.depsErr = load.ListDeps(dir, tags, patterns) })
	}
}

// changes returns the diff of module dir from ref, using the prefetched result when it is for the same module.
func (p *prefetch) changes(dir, ref string) (*diff.Changes, error) {
	p.wg.Wait()
	if p.diffStart && p.dir == dir {
		return p.diff, p.diffErr
	}
	return diff.Since(dir, ref)
}

// sources returns the same as load.SourceDeps, using the prefetched list when it can.
func (p *prefetch) sources(pkgs []*load.Package) (map[string][]string, error) {
	p.wg.Wait()
	if p.deps != nil && p.depsErr == nil && len(pkgs) > 0 && p.dir == pkgs[0].ModuleDir {
		if sources, err := p.deps.Sources(pkgs); err == nil {
			return sources, nil
		}
	}
	return load.SourceDeps(pkgs)
}
