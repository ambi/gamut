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
	"strings"
	"time"

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
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: gamut [flags] <package-directory>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	types, err := parseTypes(*only)
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	start := time.Now()
	logw := stderr
	if *quiet {
		logw = io.Discard
	}

	pkg, err := load.Load(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 1
	}
	mutants := mutant.Enumerate(pkg, types)
	mutant.CheckViability(pkg, mutants)
	fmt.Fprintf(logw, "load: %s, %d mutants in %s\n", pkg.ImportPath, len(mutants), time.Since(start).Round(time.Millisecond))
	if *dryRun {
		for _, m := range mutants {
			fmt.Fprintf(stdout, "%s:%d:%d\t%s\t%s->%s\tviable=%t\t%s\n", m.File.Rel, m.Line, m.Column, m.Type, m.Original, m.Replacement, m.Viable, m.ID)
		}
		return 0
	}

	results, err := run.Run(ctx, pkg, mutants, run.Options{
		Workers: *workers, TimeoutFactor: *timeoutFactor, TimeoutMin: *timeoutMin, Log: logw,
	})
	if err != nil {
		fmt.Fprintln(stderr, "gamut:", err)
		return 1
	}
	rep := report.Build(pkg.ModulePath, pkg.ImportPath, results, time.Since(start))
	if *output != "" {
		if err := writeReport(*output, rep); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return 1
		}
	}
	report.WriteSummary(stdout, rep, results)
	return 0
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
