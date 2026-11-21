// Command gamut は、Go のパッケージにミューテーションテストを行う。
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

// 終了コードである。
const (
	exitOK = 0
	// exitError は、引数、設定、読み込み、ビルドなどの誤りである。
	exitError = 1
	// exitQuality は、判定を終えたが品質の条件（重要領域、閾値、-fail-on-survived）を満たさないことである。
	exitQuality = 2
	// exitBaseline は、変異なしでテストが失敗し、判定しなかったことである。
	exitBaseline = 4
	// exitSelfCheck は、forced fail の自己検査で変異がテストに届かないと分かり、判定しなかったことである。
	exitSelfCheck = 5
)

func realMain(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "accept" {
		return acceptMain(args[1:], stdout, stderr)
	}
	return judge(args, stdout, stderr, false)
}

// acceptMain は、gamut accept を実行する。
// 識別子を渡せば、モジュールのルートの受理の記録に足す。-prune なら、残りの引数で判定し、要らなくなった受理を消す。
func acceptMain(args []string, stdout, stderr io.Writer) int {
	// -prune の後ろは判定のフラグとパターンなので、accept のフラグとして解釈しない。
	if len(args) > 0 && (args[0] == "-prune" || args[0] == "--prune") {
		return judge(args[1:], stdout, stderr, true)
	}
	fs := flag.NewFlagSet("gamut accept", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reason := fs.String("reason", "", "why the mutants are equivalent (required unless -prune)")
	dir := fs.String("dir", ".", "a directory inside the module whose "+accept.FileName+" to update")
	// -prune は先頭でだけ受け付ける。ここでは使い方に載せるために定義する。
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

// judge は、パッケージの変異体を判定する。prune なら、判定の後に、要らなくなった受理を受理の記録から消す。
func judge(args []string, stdout, stderr io.Writer, prune bool) int {
	fs := flag.NewFlagSet("gamut", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workers := fs.Int("workers", max(1, runtime.NumCPU()/2), "number of parallel test processes")
	only := fs.String("only", "", "comma-separated mutator types to run, opt-in ones included (default: the default set; see -list-mutators)")
	disable := fs.String("disable", "", "comma-separated mutator types to remove from the default set; cannot be combined with -only")
	listMutators := fs.Bool("list-mutators", false, "print every mutator type with its description, example, and whether it is in the default set, then exit")
	output := fs.String("o", "", "write the JSON report to this path")
	dryRun := fs.Bool("dry-run", false, "list mutants and their viability without running tests")
	timeoutFactor := fs.Float64("timeout-factor", 5, "multiply the selected tests' baseline time by this for the per-mutant timeout")
	timeoutMin := fs.Duration("timeout-min", 2*time.Second, "add this to every per-mutant timeout")
	quiet := fs.Bool("q", false, "print only the summary")
	tags := fs.String("tags", "", "comma-separated build tags for loading, building, and type-checking")
	testFlags := fs.String("test-flags", "", "space-separated flags for every test binary run, such as \"-short\"")
	cachePath := fs.String("cache", filepath.Join(".gamut", "cache.json"), "read and write reusable verdicts at this path")
	noCache := fs.Bool("no-cache", false, "neither reuse nor record verdicts and coverage")
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
	fs.Var(&reporterKinds, "reporter", "report format: text (the summary on stdout), gomutants (JSON), json (mutation-testing-elements), html, or agent (one JSON document on stdout for coding agents) (repeatable; default text, plus gomutants with -o)")
	reportDir := fs.String("report-dir", filepath.Join(".gamut", "report"), "write the json, html, and gomutants (without -o) reports to this directory")
	annotations := fs.String("annotations", "", "also print the survivors to fix as CI annotations on stdout (values: github)")
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
	if *listMutators {
		writeMutators(stdout)
		return exitOK
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
	// 設定ファイルの値は、指定されなかったフラグにだけ入れる。
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
	// 選択のない実行は、入力が前回と同じなら、読み込みもビルドもせずに前回の報告を返す。
	quickable := !*noCache && !*dryRun && len(ids) == 0 && *since == "" && !prune
	var early *cache.Store
	if !*noCache && earlyDir != "" {
		if early, err = openStore(*cachePath, earlyDir, settings); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
	}
	var quickKey string
	if quickable {
		// 報告を変えるフラグはすべて鍵に入れる。品質の条件の -fail-on-survived は報告を変えず、再生した報告に当てる。
		quickKey = cache.QuickKey(slices.Concat(abs, settings, []string{
			fmt.Sprintf("integration=%t", *integration), fmt.Sprintf("detect-equivalent=%t", *detectEquivalent),
			"exclude=" + strings.Join(excludes, "\x00"), "critical=" + strings.Join(critical, "\x00"),
			fmt.Sprintf("arid=%t,%s,%s", *noArid, strings.Join(aridCalls, "\x00"), strings.Join(aridPackages, "\x00")),
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
	// git diff と依存の一覧は、読み込みを待たずに始める。モジュールが読み込みの結果と違えば、捨てて求め直す。
	var pre prefetch
	if earlyDir != "" && !*dryRun {
		pre.start(earlyDir, splitTags(*tags), abs, *since, !*noCache || *since != "")
	}

	// 前回の読み込みでエクスポートデータを求めたパッケージを、依存の一覧と並行して求める。
	loadKey := cache.QuickKey(slices.Concat([]string{"load", *tags}, abs)...)
	var hint []string
	if early != nil {
		hint = early.LoadHint(loadKey)
	}
	pkgs, hint, err := load.WithHint(fs.Args(), splitTags(*tags), hint)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 1
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
	// 差分が変えたテストを求めるのにも鍵を使うので、キャッシュを使わなくても -since では鍵を計算する。
	var sources map[string][]string
	if !*noCache || changes != nil {
		if sources, err = computeKeys(pkgs, targets, !*noCache, &pre); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
	}
	var quick *cache.Quick
	if quickable {
		// 入力の状態は、テストを実行する前に取る。実行中にファイルが変わっても、変わる前の状態を記録しない。
		if quick, err = quickInputs(fs.Args(), targets, sources, accepted.Path()); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
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
		// 取り消しや基準のテストの失敗で止まっても、それまでに実行した判定を残す。
		defer func() {
			if err := store.Save(); err != nil {
				fmt.Fprintln(stderr, "gamut: warning: saving the cache:", err)
			}
		}()
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
			// 識別子で指定したときは、指定した変異体だけを報告する。
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
	rep.AddIgnored(ignored)
	rep.MutantsArid = aridSkipped
	rep.MarkCritical(criticalIDs)
	rep.Thresholds = &thresholds
	// 識別子で指定したときは、ほかの変異体を報告しないので、受理の「なくなった」は判定できない。
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
	if quick != nil && opts.Cache != nil {
		var buf strings.Builder
		if err := rep.WriteJSON(&buf); err == nil {
			quick.Report = json.RawMessage(buf.String())
			opts.Cache.PutQuick(quickKey, quick)
		}
	}
	return qualityCode(rep, *failOnSurvived, stderr)
}

// qualityCode は、判定を終えた報告が品質の条件を満たすかを確かめ、満たさなければ理由を書いて exitQuality を返す。
func qualityCode(rep *report.Report, failOnSurvived bool, stderr io.Writer) int {
	failures := rep.Quality(failOnSurvived)
	if len(failures) == 0 {
		return exitOK
	}
	fmt.Fprintln(stderr, "gamut: quality gate failed: "+strings.Join(failures, "; "))
	return exitQuality
}

// checkThresholds は、閾値の組み合わせを確かめ、誤りがあればその説明を返す。
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

// openStore は、moduleDir の環境の下でキャッシュを開く。
func openStore(path, moduleDir string, settings []string) (*cache.Store, error) {
	env, err := cache.Environment(moduleDir, settings...)
	if err != nil {
		return nil, err
	}
	return cache.Open(path, env)
}

// replay は、key の前回の実行から入力が変わっていなければ、その報告を返す。2 つ目の戻り値は、報告を返せたかである。
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

// 報告の形式である。
const (
	reporterText      = "text"
	reporterGomutants = "gomutants"
	reporterJSON      = "json"
	reporterHTML      = "html"
	reporterAgent     = "agent"
)

// reporters は、報告の出し方である。
type reporters struct {
	kinds []string
	// output は -o の書き出し先で、gomutants の形式で書く。dir は -report-dir で、ほかのファイルの形式の書き出し先である。
	output, dir string
	// annotations は CI の注釈の形式（github）で、空なら書かない。
	annotations string
}

// newReporters は、-reporter の指定から報告の出し方を作る。指定がなければ text にする。
// -o を指定すれば、-reporter によらず gomutants の形式をそこへ書く。
func newReporters(kinds []string, output, dir, annotations string) (*reporters, error) {
	if annotations != "" && annotations != "github" {
		return nil, fmt.Errorf("unknown -annotations %q; use github", annotations)
	}
	if len(kinds) == 0 {
		kinds = []string{reporterText}
	}
	for _, k := range kinds {
		if !slices.Contains([]string{reporterText, reporterGomutants, reporterJSON, reporterHTML, reporterAgent}, k) {
			return nil, fmt.Errorf("unknown -reporter %q; use text, gomutants, json, html, or agent", k)
		}
	}
	// agent は標準出力に JSON を 1 つだけ書くので、標準出力に書くほかの形式と混ぜない。
	if slices.Contains(kinds, reporterAgent) && (slices.Contains(kinds, reporterText) || annotations != "") {
		return nil, errors.New("-reporter agent writes one JSON document to stdout; it cannot be combined with -reporter text or -annotations")
	}
	if output != "" && !slices.Contains(kinds, reporterGomutants) {
		kinds = append(kinds, reporterGomutants)
	}
	return &reporters{kinds: kinds, output: output, dir: dir, annotations: annotations}, nil
}

// repoPrefix は、Git のリポジトリのルートから moduleDir への相対パスを返す。リポジトリの外なら空を返す。
// CI の注釈のファイルはリポジトリ相対で指定するので、モジュール相対のパスの前に付ける。
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

// deliver は、報告を指定の形式で書く。mutation-testing-elements の形式（json、html）は、ファイルの中身を moduleDir から読む。
func (o *reporters) deliver(rep *report.Report, moduleDir string, stdout io.Writer) error {
	var mte *report.MTEReport
	for _, k := range o.kinds {
		var err error
		switch k {
		case reporterText:
			report.WriteSummary(stdout, rep)
		case reporterAgent:
			out, aerr := rep.Agent(moduleDir)
			if aerr != nil {
				return aerr
			}
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			err = enc.Encode(out)
		case reporterGomutants:
			path := o.output
			if path == "" {
				path = filepath.Join(o.dir, "gomutants.json")
			}
			err = writeReport(path, rep)
		case reporterJSON, reporterHTML:
			if mte == nil {
				if mte, err = rep.MTE(moduleDir); err != nil {
					return err
				}
			}
			if k == reporterJSON {
				err = writeFile(filepath.Join(o.dir, "mutation.json"), func(w io.Writer) error {
					enc := json.NewEncoder(w)
					enc.SetIndent("", "  ")
					return enc.Encode(mte)
				})
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

// quickInputs は、前回の報告をそのまま返してよいかを次の実行で確かめるための、入力のファイルとディレクトリを集め、今の状態の指紋を取る。
// 対象パッケージと依存ファイルのディレクトリの直下のファイル、依存ファイル、<ディレクトリ>/... のパターンの根の下の木である。
// go.mod、go.sum、go.work、gamut、Go の版は、キャッシュの環境の鍵が捉える。
// extra は、そのほかに状態を比べるファイル（受理の記録など）である。
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

// checkViability は、変異体の構築可能性を判定する。store があれば、鍵の変わらない変異体はその記録を使い、判定を記録する。
// 型検査した変異体の数を返す。
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

// computeKeys は、targets に再利用の鍵を付ける。withRevDeps なら、逆依存パッケージのテストの鍵も付ける。
// パッケージごとの外の依存のファイルを返す。
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

// openCache は、キャッシュを読み、名前を変えたファイルの変異体に前の判定を引き継ぎ、消えた変異体の記録を捨てる。
// opened が nil でなければ、同じモジュールの環境で開いたものとして使う。
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

// resolvePath は、シンボリックリンクを解決したパスを返す。解決できなければそのまま返す。
func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// resolveMutants は、識別子か、識別子の一意な前方一致から変異体を求める。
// 当たらないか、複数に当たる指定があれば、候補を並べた誤りを返す。
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

// checkSelectionFlags は、選択のフラグの組み合わせを確かめ、誤りがあればその説明を返す。
// acceptedInPackages は、受理のうち、判定するパッケージのファイルを指すものの識別子を返す。
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

// readConfig は、path が空でなければそのファイルを、空なら moduleDir の設定ファイルを読む。どちらもなければ nil を返す。
func readConfig(path, moduleDir string) (*config.Config, error) {
	switch {
	case path != "":
		return config.Load(path)
	case moduleDir != "":
		return config.Find(moduleDir)
	}
	return nil, nil
}

// applyConfig は、設定の値を、コマンドラインで指定されなかったフラグに入れる。
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

// compileExcludes は、-exclude-files の正規表現をコンパイルする。
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

// excluded は、モジュール相対のパス rel が、どれかの正規表現に当たるかを返す。
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

// multiFlag は、繰り返し指定できる文字列のフラグである。
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

// exitCode は、実行の誤りを終了コードへ対応させる。
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

// selectTypes は、-only と -disable から有効な変異演算子を決める。どちらもなければ既定の集合である。
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

// writeMutators は、変異演算子の一覧を書く。
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

func writeReport(path string, rep *report.Report) error {
	return writeFile(path, rep.WriteJSON)
}

// writeFile は、path に write の書いた内容を書く。ディレクトリがなければ作る。
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

// prefetch は、読み込みと並行して始める git diff と依存の一覧である。
type prefetch struct {
	dir       string
	wg        sync.WaitGroup
	deps      *load.Deps
	depsErr   error
	diff      *diff.Changes
	diffErr   error
	diffStart bool
}

// start は、モジュール dir で、ref が空でなければ git diff を、withDeps なら依存の一覧を始める。
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

// changes は、モジュール dir の ref からの差分を返す。先に始めた結果が同じモジュールのものなら、それを使う。
func (p *prefetch) changes(dir, ref string) (*diff.Changes, error) {
	p.wg.Wait()
	if p.diffStart && p.dir == dir {
		return p.diff, p.diffErr
	}
	return diff.Since(dir, ref)
}

// sources は、load.SourceDeps と同じものを返す。先に始めた一覧が使えれば、それを使う。
func (p *prefetch) sources(pkgs []*load.Package) (map[string][]string, error) {
	p.wg.Wait()
	if p.deps != nil && p.depsErr == nil && len(pkgs) > 0 && p.dir == pkgs[0].ModuleDir {
		if sources, err := p.deps.Sources(pkgs); err == nil {
			return sources, nil
		}
	}
	return load.SourceDeps(pkgs)
}
