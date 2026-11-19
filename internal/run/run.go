// Package run は、schemata のテストバイナリをビルドし、被覆を記録し、変異体を判定する。
package run

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/schemata"
)

// Status は判定の状態である。名前は gomutants の報告に合わせる。
type Status string

// 判定の状態。
const (
	Killed     Status = "KILLED"
	Lived      Status = "LIVED"
	NotCovered Status = "NOT COVERED"
	NotViable  Status = "NOT VIABLE"
	TimedOut   Status = "TIMED OUT"
)

// 生き残りの理由。
const (
	ReasonNotInfected = "not infected"
	ReasonNotDetected = "not detected"
)

// 判定の方式。
const (
	ModeSchemata   = "schemata"
	ModeStandalone = "standalone"
)

// Result は 1 体の変異体の判定である。
type Result struct {
	Mutant   *mutant.Mutant
	Status   Status
	Reason   string
	Mode     string
	Tests    []string
	KilledBy string
	Elapsed  time.Duration
}

// Options は実行の設定である。
type Options struct {
	Workers int
	// TimeoutFactor は、選んだテストの基準の実行時間に掛ける係数である。
	TimeoutFactor float64
	// TimeoutMin は時間切れの上限に足す固定の時間である。
	TimeoutMin time.Duration
	// Log は進行状況の出力先である。nil なら出さない。
	Log io.Writer
	// Warn は警告の出力先である。nil なら出さない。
	Warn io.Writer

	// rewriteHelper はテスト専用の差し替え口で、補助ファイルの内容をビルドの前に書き換える。
	rewriteHelper func([]byte) []byte
}

// ErrSelfCheck は、変異を有効にしてもテストが失敗せず、変異がテストに届いていないと分かったことを表す。
// このまま判定すると、すべての変異体が生き残りになり、テストの弱さと見分けが付かない。
var ErrSelfCheck = errors.New("forced-fail self-check failed")

type testCase struct {
	name     string
	duration time.Duration
	hits     map[int]bool
	infected map[int]bool
}

// Run は mutants を判定する。
func Run(ctx context.Context, pkg *load.Package, mutants []*mutant.Mutant, opts Options) ([]*Result, error) {
	if opts.Workers < 1 {
		opts.Workers = 1
	}
	logf := func(format string, args ...any) {
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, format+"\n", args...)
		}
	}

	results := make([]*Result, len(mutants))
	var viable []*mutant.Mutant
	for i, m := range mutants {
		if !m.Viable {
			results[i] = &Result{Mutant: m, Status: NotViable}
			continue
		}
		viable = append(viable, m)
	}

	sch, err := schemata.Build(pkg, mutants)
	if err != nil {
		return nil, err
	}
	logf("schemata: %d embedded, %d standalone with coverage, %d standalone without coverage, %d not viable",
		sch.Count(schemata.Wrapped), sch.Count(schemata.Marked), sch.Count(schemata.Untouched), len(mutants)-len(viable))

	work, err := os.MkdirTemp("", "gamut-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(work) }()

	helper := sch.Helper
	if opts.rewriteHelper != nil {
		helper = opts.rewriteHelper(helper)
	}
	overlay := map[string][]byte{sch.HelperPath: helper}
	maps.Copy(overlay, sch.Files)
	bin := filepath.Join(work, "schemata.test")
	start := time.Now()
	if err := buildTest(ctx, pkg.Dir, work, "schemata", overlay, bin); err != nil {
		return nil, err
	}
	logf("build: %s", time.Since(start).Round(time.Millisecond))

	names, err := listTests(ctx, pkg.Dir, bin)
	if err != nil {
		return nil, err
	}
	start = time.Now()
	tests, err := cover(ctx, pkg.Dir, bin, work, names, opts.Workers)
	if err != nil {
		return nil, err
	}
	logf("coverage: %d tests in %s", len(tests), time.Since(start).Round(time.Millisecond))
	if err := selfCheck(ctx, pkg.Dir, bin, work, tests, opts); err != nil {
		return nil, err
	}

	type job struct {
		index int
		run   func() *Result
	}
	var jobs []job
	for i, m := range mutants {
		if results[i] != nil {
			continue
		}
		site := sch.SiteOf[m]
		if site.Level == schemata.Untouched {
			jobs = append(jobs, job{i, func() *Result {
				return standalone(ctx, pkg, m, work, tests, opts)
			}})
			continue
		}
		var reached, infected []*testCase
		for _, t := range tests {
			if t.hits[site.Index] {
				reached = append(reached, t)
				if t.infected[m.Index] {
					infected = append(infected, t)
				}
			}
		}
		switch {
		case len(reached) == 0:
			results[i] = &Result{Mutant: m, Status: NotCovered, Mode: modeOf(site)}
		case site.Level == schemata.Marked:
			jobs = append(jobs, job{i, func() *Result {
				r := standalone(ctx, pkg, m, work, reached, opts)
				r.Tests = testNames(reached)
				return r
			}})
		case len(infected) == 0:
			results[i] = &Result{Mutant: m, Status: Lived, Reason: ReasonNotInfected, Mode: ModeSchemata, Tests: testNames(reached)}
		default:
			jobs = append(jobs, job{i, func() *Result {
				r := execute(ctx, pkg.Dir, bin, m.Index, infected, opts)
				r.Mutant, r.Mode, r.Tests = m, ModeSchemata, testNames(reached)
				return r
			}})
		}
	}

	start = time.Now()
	var mu sync.Mutex
	done := 0
	parallel(len(jobs), opts.Workers, func(k int) {
		r := jobs[k].run()
		mu.Lock()
		results[jobs[k].index] = r
		done++
		if opts.Log != nil && (done%50 == 0 || done == len(jobs)) {
			logf("mutants: %d/%d executed", done, len(jobs))
		}
		mu.Unlock()
	})
	logf("execute: %d mutants in %s", len(jobs), time.Since(start).Round(time.Millisecond))
	return results, ctx.Err()
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

// buildTest は overlay を当てたテストバイナリを bin へビルドする。
func buildTest(ctx context.Context, dir, work, name string, files map[string][]byte, bin string) error {
	srcDir := filepath.Join(work, name+"-src")
	if err := os.MkdirAll(srcDir, 0o700); err != nil {
		return err
	}
	replace := map[string]string{}
	keys := make([]string, 0, len(files))
	for path := range files {
		keys = append(keys, path)
	}
	slices.Sort(keys)
	for i, path := range keys {
		dst := filepath.Join(srcDir, strconv.Itoa(i)+"_"+filepath.Base(path))
		if err := os.WriteFile(dst, files[path], 0o600); err != nil {
			return err
		}
		replace[path] = dst
	}
	overlay, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return err
	}
	overlayPath := filepath.Join(work, name+"-overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "go", "test", "-c", "-o", bin, "-overlay", overlayPath, ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build test binary: %w\n%s", err, out)
	}
	return nil
}

var runnable = regexp.MustCompile(`^(Test|Example|Fuzz)`)

func listTests(ctx context.Context, dir, bin string) ([]string, error) {
	cmd := exec.CommandContext(ctx, bin, "-test.list", ".")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list tests: %w", err)
	}
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		name := strings.TrimSpace(sc.Text())
		if runnable.MatchString(name) {
			names = append(names, name)
		}
	}
	return names, nil
}

// cover は、変異を無効にしたまま各テストを別のプロセスで実行し、到達と感染を記録する。
func cover(ctx context.Context, dir, bin, work string, names []string, workers int) ([]*testCase, error) {
	tests := make([]*testCase, len(names))
	errs := make([]error, len(names))
	parallel(len(names), workers, func(i int) {
		path := filepath.Join(work, "cover-"+strconv.Itoa(i))
		start := time.Now()
		cmd := exec.CommandContext(ctx, bin, "-test.run", "^"+regexp.QuoteMeta(names[i])+"$", "-test.count=1", "-test.paniconexit0")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GAMUT_COVER="+path, "GAMUT_MUTANT=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			errs[i] = fmt.Errorf("test %s fails without mutants, so verdicts would be meaningless: %w\n%s", names[i], err, out)
			return
		}
		t := &testCase{name: names[i], duration: time.Since(start), hits: map[int]bool{}, infected: map[int]bool{}}
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			errs[i] = err
			return
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
			}
		}
		tests[i] = t
	})
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return tests, nil
}

// selfCheck は、到達の記録があるテストのうち最も短い 1 件を forced fail で実行し、失敗することを確かめる。
// 到達が記録されたのに失敗しないなら、GAMUT_MUTANT がテストのプロセスで読まれていない。
func selfCheck(ctx context.Context, dir, bin, work string, tests []*testCase, opts Options) error {
	var probe *testCase
	for _, t := range tests {
		if len(t.hits) > 0 && (probe == nil || t.duration < probe.duration) {
			probe = t
		}
	}
	if probe == nil {
		if opts.Warn != nil {
			fmt.Fprintln(opts.Warn, "gamut: warning: forced-fail self-check skipped because no test reaches a mutated site")
		}
		return nil
	}
	limit := time.Duration(float64(probe.duration)*opts.TimeoutFactor) + opts.TimeoutMin
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(runCtx, bin, "-test.run", "^"+regexp.QuoteMeta(probe.name)+"$", "-test.count=1", "-test.paniconexit0")
	cmd.Dir = dir
	// gamutHit は被覆の記録中にだけ呼ばれるので、記録を有効にして forced fail を渡す。
	cmd.Env = append(os.Environ(), "GAMUT_COVER="+filepath.Join(work, "forced-fail"), "GAMUT_MUTANT="+strconv.Itoa(schemata.ForcedFail))
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case runCtx.Err() != nil:
		return fmt.Errorf("%w: %s did not finish within %s with GAMUT_MUTANT=%d", ErrSelfCheck, probe.name, limit, schemata.ForcedFail)
	case err == nil:
		return fmt.Errorf("%w: %s reaches mutated sites but passed with GAMUT_MUTANT=%d; "+
			"the test process does not read the mutant selection, so every mutant would survive "+
			"(check that GAMUT_MUTANT reaches the test binary and that the code under test does not recover the forced panic)\n%s",
			ErrSelfCheck, probe.name, schemata.ForcedFail, out)
	}
	if opts.Log != nil {
		fmt.Fprintf(opts.Log, "self-check: forced fail failed %s in %s\n", probe.name, time.Since(start).Round(time.Millisecond))
	}
	return nil
}

var failLine = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)

// execute は、変異体 index を有効にして tests を短い順に実行する。
func execute(ctx context.Context, dir, bin string, index int, tests []*testCase, opts Options) *Result {
	sorted := slices.Clone(tests)
	slices.SortStableFunc(sorted, func(a, b *testCase) int { return int(a.duration - b.duration) })
	var total time.Duration
	quoted := make([]string, len(sorted))
	for i, t := range sorted {
		total += t.duration
		quoted[i] = regexp.QuoteMeta(t.name)
	}
	limit := time.Duration(float64(total)*opts.TimeoutFactor) + opts.TimeoutMin
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(runCtx, bin, "-test.run", "^(?:"+strings.Join(quoted, "|")+")$", "-test.count=1", "-test.failfast", "-test.paniconexit0")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GAMUT_MUTANT="+strconv.Itoa(index))
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	r := &Result{Elapsed: time.Since(start)}
	switch {
	case runCtx.Err() != nil && ctx.Err() == nil:
		r.Status = TimedOut
	case err != nil:
		r.Status = Killed
		if m := failLine.FindSubmatch(out); m != nil {
			r.KilledBy = string(m[1])
		}
	default:
		r.Status, r.Reason = Lived, ReasonNotDetected
	}
	return r
}

// standalone は、schemata に埋め込めなかった変異体を個別にビルドし、全テストで判定する。
func standalone(ctx context.Context, pkg *load.Package, m *mutant.Mutant, work string, tests []*testCase, opts Options) *Result {
	name := "standalone-" + strconv.Itoa(m.Index)
	bin := filepath.Join(work, name+".test")
	defer func() { _ = os.Remove(bin) }()
	err := buildTest(ctx, pkg.Dir, work, name, map[string][]byte{m.File.Path: m.Apply(m.File.Src)}, bin)
	if err != nil {
		// 型検査を通ってもコンパイラが拒む変異体（定数の桁あふれなど）は構築不能とする。
		return &Result{Mutant: m, Status: NotViable, Mode: ModeStandalone}
	}
	r := execute(ctx, pkg.Dir, bin, 0, tests, opts)
	r.Mutant, r.Mode = m, ModeStandalone
	return r
}

func modeOf(s *schemata.Site) string {
	if s.Level == schemata.Wrapped {
		return ModeSchemata
	}
	return ModeStandalone
}
