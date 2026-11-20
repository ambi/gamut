// Command gamut は、Go のパッケージにミューテーションテストを行う。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/report"
	"github.com/ambi/gamut/internal/run"
)

func main() {
	os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr))
}

func realMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gamut", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workers := fs.Int("workers", 2, "number of parallel test processes")
	only := fs.String("only", "", "comma-separated mutator types to run (default: all)")
	output := fs.String("o", "", "write the JSON report to this path")
	dryRun := fs.Bool("dry-run", false, "list mutants and their viability without running tests")
	timeoutFactor := fs.Float64("timeout-factor", 5, "multiply the selected tests' baseline time by this for the per-mutant timeout")
	timeoutMin := fs.Duration("timeout-min", 2*time.Second, "add this to every per-mutant timeout")
	quiet := fs.Bool("q", false, "print only the summary")
	tags := fs.String("tags", "", "comma-separated build tags for loading, building, and type-checking")
	testFlags := fs.String("test-flags", "", "space-separated flags for every test binary run, such as \"-short\"")
	cachePath := fs.String("cache", filepath.Join(".gamut", "cache.json"), "read and write reusable verdicts at this path")
	noCache := fs.Bool("no-cache", false, "neither reuse nor record verdicts")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: gamut [flags] <package-directory>[/...] ...")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	types, err := parseTypes(*only)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 2
	}
	flags, err := run.ParseTestFlags(*testFlags)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	start := time.Now()
	logw := stderr
	if *quiet {
		logw = io.Discard
	}

	pkgs, err := load.Load(fs.Args(), splitTags(*tags))
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 1
	}
	targets := make([]run.Target, len(pkgs))
	paths := make([]string, len(pkgs))
	total := 0
	for i, pkg := range pkgs {
		mutants := mutant.Enumerate(pkg, types)
		mutant.CheckViability(pkg, mutants)
		targets[i] = run.Target{Pkg: pkg, Mutants: mutants}
		paths[i] = pkg.ImportPath
		total += len(mutants)
		fmt.Fprintf(logw, "load: %s, %d mutants\n", pkg.ImportPath, len(mutants))
	}
	fmt.Fprintf(logw, "load: %d packages, %d mutants in %s\n", len(pkgs), total, time.Since(start).Round(time.Millisecond))
	if *dryRun {
		for _, t := range targets {
			for _, m := range t.Mutants {
				fmt.Fprintf(stdout, "%s:%d:%d\t%s\t%s->%s\tviable=%t\t%s\n", m.File.Rel, m.Line, m.Column, m.Type, m.Original, m.Replacement, m.Viable, m.ID)
			}
		}
		return 0
	}

	opts := run.Options{
		Workers: *workers, TimeoutFactor: *timeoutFactor, TimeoutMin: *timeoutMin, Log: logw, Warn: stderr, TestFlags: flags,
	}
	if !*noCache {
		settings := []string{
			"types=" + strings.Join(slices.Sorted(slices.Values(typeNames(types))), ","),
			"tags=" + *tags,
			"test-flags=" + strings.Join(flags, " "),
			fmt.Sprintf("timeout=%g,%s", *timeoutFactor, *timeoutMin),
		}
		store, err := openCache(*cachePath, settings, pkgs, targets)
		if err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
		if store.Reset != "" {
			fmt.Fprintf(logw, "cache: discarding previous verdicts because %s\n", store.Reset)
		}
		opts.Cache = store
		// 取り消しや基準のテストの失敗で止まっても、それまでに実行した判定を残す。
		defer func() {
			if err := store.Save(); err != nil {
				fmt.Fprintln(stderr, "gamut: warning: saving the cache:", err)
			}
		}()
	}

	perPackage, err := run.All(ctx, targets, opts)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return exitCode(err)
	}
	var results []*run.Result
	for _, rs := range perPackage {
		results = append(results, rs...)
	}
	label := paths[0]
	if len(pkgs) > 1 {
		label = strings.Join(fs.Args(), " ")
	}
	rep := report.Build(pkgs[0].ModulePath, label, paths, results, time.Since(start))
	if *output != "" {
		if err := writeReport(*output, rep); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
	}
	report.WriteSummary(stdout, rep, results)
	return 0
}

// openCache は、キャッシュを読み、targets に再利用の鍵を付け、消えた変異体の記録を捨てる。
func openCache(path string, settings []string, pkgs []*load.Package, targets []run.Target) (*cache.Store, error) {
	env, err := cache.Environment(pkgs[0].ModuleDir, settings...)
	if err != nil {
		return nil, err
	}
	sources, err := load.SourceDeps(pkgs)
	if err != nil {
		return nil, err
	}
	store, err := cache.Open(path, env)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	paths := make([]string, len(targets))
	for i := range targets {
		pkg := targets[i].Pkg
		paths[i] = pkg.ImportPath
		if targets[i].Keys, err = cache.NewKeys(pkg, sources[pkg.ImportPath]); err != nil {
			return nil, err
		}
		for _, m := range targets[i].Mutants {
			live[m.ID] = true
		}
	}
	store.Prune(paths, func(id string) bool { return live[id] })
	return store, nil
}

func typeNames(types []mutant.Type) []string {
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = string(t)
	}
	return out
}

// exitCode は、実行の誤りを終了コードへ対応させる。
// 4 は、基準のテストが失敗したか、変異がテストに届かないと自己検査で分かり、判定しなかったことを表す。
func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, run.ErrBaseline), errors.Is(err, run.ErrSelfCheck):
		return 4
	}
	return 1
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

func parseTypes(s string) ([]mutant.Type, error) {
	if s == "" {
		return mutant.AllTypes, nil
	}
	var out []mutant.Type
	for name := range strings.SplitSeq(s, ",") {
		t := mutant.Type(strings.TrimSpace(name))
		found := false
		for _, known := range mutant.AllTypes {
			if t == known {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown mutator type %q", t)
		}
		out = append(out, t)
	}
	return out, nil
}

func writeReport(path string, rep *report.Report) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return rep.WriteJSON(f)
}
