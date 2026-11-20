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

	"github.com/ambi/gamut/internal/cache"
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
	// Cached は、判定を実行せずにキャッシュから再利用したかである。
	Cached bool
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
	// TestFlags は、テストバイナリのすべての実行に渡すフラグである。ParseTestFlags で作る。
	TestFlags []string
	// Cache は判定の再利用に使うキャッシュである。nil なら再利用も記録もしない。
	// 鍵を持たない Target（Keys が nil）の変異体も再利用しない。
	Cache *cache.Store

	// rewriteHelper はテスト専用の差し替え口で、補助ファイルの内容をビルドの前に書き換える。
	rewriteHelper func([]byte) []byte
	// afterJob はテスト専用の差し替え口で、変異体の実行が 1 体終わるたびに呼ばれる。
	afterJob func()
}

// ErrBaseline は、変異を無効にした状態でテストが失敗し、判定の前提が崩れていることを表す。
var ErrBaseline = errors.New("baseline tests fail without mutants")

// ErrSelfCheck は、変異を有効にしてもテストが失敗せず、変異がテストに届いていないと分かったことを表す。
// このまま判定すると、すべての変異体が生き残りになり、テストの弱さと見分けが付かない。
var ErrSelfCheck = errors.New("forced-fail self-check failed")

type testCase struct {
	name     string
	duration time.Duration
	hits     map[int]bool
	infected map[int]bool
}

// Target は、判定する 1 つのパッケージとその変異体である。
type Target struct {
	Pkg     *load.Package
	Mutants []*mutant.Mutant
	// Keys は判定の再利用の鍵である。nil ならこのパッケージの判定を再利用しない。
	Keys *cache.Keys
}

// Run は、1 つのパッケージの mutants を判定する。
func Run(ctx context.Context, pkg *load.Package, mutants []*mutant.Mutant, opts Options) ([]*Result, error) {
	results, err := All(ctx, []Target{{Pkg: pkg, Mutants: mutants}}, opts)
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

// unit は、All の中での 1 つのパッケージの状態である。
type unit struct {
	Target
	results []*Result
	work    string
	sch     *schemata.Result
	bin     string
	names   []string
	tests   []*testCase
}

type job struct {
	unit  *unit
	index int
	run   func() *Result
	// mode は判定の方式、reached は変異箇所に到達したテスト、ran は判定のために実行するテストである。
	mode    string
	reached []*testCase
	ran     []*testCase
}

// All は、targets の全変異体を判定し、targets と同じ順に判定の列を返す。
// パッケージごとのビルドを並列に行い、被覆の記録と変異体の実行は全パッケージで 1 つの作業の列にまとめてワーカーを共有する。
// どのパッケージかで基準のテストか自己検査が失敗したら、どのパッケージも判定しない。
func All(ctx context.Context, targets []Target, opts Options) ([][]*Result, error) {
	if opts.Workers < 1 {
		opts.Workers = 1
	}
	// パッケージごとの準備と自己検査は並列に進むので、出力先への書き込みを直列にする。
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
		viable := 0
		for k, m := range t.Mutants {
			if !m.Viable {
				u.results[k] = &Result{Mutant: m, Status: NotViable}
				continue
			}
			viable++
			if !t.Pkg.HasTests {
				// テストファイルがなければどのテストも到達しないので、ビルドせずに判定する。
				u.results[k] = &Result{Mutant: m, Status: NotCovered}
			}
		}
		if viable > 0 && t.Pkg.HasTests {
			active = append(active, u)
		} else if viable > 0 {
			logf("skip: %s has no test files, so %d viable mutants are not covered", t.Pkg.ImportPath, viable)
		}
	}

	errs := make([]error, len(active))
	parallel(len(active), opts.Workers, func(i int) { errs[i] = prepare(ctx, active[i], opts, logf) })
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	// 被覆の記録は、全パッケージのテストを 1 つの列にまとめる。
	type coverItem struct {
		unit *unit
		i    int
	}
	var items []coverItem
	for _, u := range active {
		u.tests = make([]*testCase, len(u.names))
		for i := range u.names {
			items = append(items, coverItem{u, i})
		}
	}
	start := time.Now()
	errs = make([]error, len(items))
	parallel(len(items), opts.Workers, func(k int) {
		it := items[k]
		it.unit.tests[it.i], errs[k] = coverTest(ctx, it.unit, it.i, opts)
	})
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	for _, u := range active {
		logf("coverage: %s: %d tests", u.Pkg.ImportPath, len(u.tests))
	}
	logf("coverage: %d tests in %s", len(items), time.Since(start).Round(time.Millisecond))

	errs = make([]error, len(active))
	parallel(len(active), opts.Workers, func(i int) { errs[i] = selfCheck(ctx, active[i], opts) })
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	var all []job
	for _, u := range active {
		all = append(all, u.jobs(ctx, opts)...)
	}
	jobs := all
	if opts.Cache != nil {
		jobs = reuse(all, opts.Cache, duplicateIDs(targets))
		logf("cache: %d mutants reused, %d to execute", len(all)-len(jobs), len(jobs))
	}
	start = time.Now()
	var mu sync.Mutex
	done := 0
	parallel(len(jobs), opts.Workers, func(k int) {
		j := jobs[k]
		r := j.run()
		// 取り消しの後に終わった実行は、テストが失敗したのか止められたのか区別できないので、記録しない。
		if ctx.Err() == nil {
			j.record(opts.Cache, r)
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
	logf("execute: %d mutants in %s", len(jobs), time.Since(start).Round(time.Millisecond))

	out := make([][]*Result, len(units))
	for i, u := range units {
		out[i] = u.results
	}
	return out, ctx.Err()
}

// lockedWriter は、mu を持って w へ書く。Log と Warn が同じ出力先でも行が混ざらないよう、mu を共有する。
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// prepare は、パッケージの schemata を書き換え、テストバイナリをビルドし、テストの一覧を得る。
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
	overlay := map[string][]byte{sch.HelperPath: helper}
	maps.Copy(overlay, sch.Files)
	u.bin = filepath.Join(u.work, "schemata.test")
	start := time.Now()
	if err := buildTest(ctx, pkg, u.work, "schemata", overlay, u.bin); err != nil {
		return err
	}
	logf("build: %s in %s", pkg.ImportPath, time.Since(start).Round(time.Millisecond))
	u.names, err = listTests(ctx, pkg.Dir, u.bin, opts.TestFlags)
	return err
}

// jobs は、被覆の記録から判定できる変異体の判定を書き、実行が要る変異体の作業を返す。
func (u *unit) jobs(ctx context.Context, opts Options) []job {
	pkg, sch, tests := u.Pkg, u.sch, u.tests
	var jobs []job
	for i, m := range u.Mutants {
		if u.results[i] != nil {
			continue
		}
		site := sch.SiteOf[m]
		if site.Level == schemata.Untouched {
			jobs = append(jobs, job{unit: u, index: i, mode: ModeStandalone, ran: tests, run: func() *Result {
				return standalone(ctx, pkg, m, u.work, tests, opts)
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
			u.results[i] = &Result{Mutant: m, Status: NotCovered, Mode: modeOf(site)}
		case site.Level == schemata.Marked:
			jobs = append(jobs, job{unit: u, index: i, mode: ModeStandalone, reached: reached, ran: reached, run: func() *Result {
				r := standalone(ctx, pkg, m, u.work, reached, opts)
				r.Tests = testNames(reached)
				return r
			}})
		case len(infected) == 0:
			u.results[i] = &Result{Mutant: m, Status: Lived, Reason: ReasonNotInfected, Mode: ModeSchemata, Tests: testNames(reached)}
		default:
			jobs = append(jobs, job{unit: u, index: i, mode: ModeSchemata, reached: reached, ran: infected, run: func() *Result {
				r := execute(ctx, pkg.Dir, u.bin, m.Index, infected, opts)
				r.Mutant, r.Mode, r.Tests = m, ModeSchemata, testNames(reached)
				return r
			}})
		}
	}
	return jobs
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
func buildTest(ctx context.Context, pkg *load.Package, work, name string, files map[string][]byte, bin string) error {
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
	args := []string{"test", "-c", "-o", bin, "-overlay", overlayPath}
	if len(pkg.Tags) > 0 {
		args = append(args, "-tags="+strings.Join(pkg.Tags, ","))
	}
	cmd := exec.CommandContext(ctx, "go", append(args, ".")...)
	cmd.Dir = pkg.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build test binary: %w\n%s", err, out)
	}
	return nil
}

// testCommand は、テストバイナリを起動するコマンドを作る。
// 利用者のフラグを先に、gamut が実行ごとに決めるフラグを後に置く。
// テストバイナリは新しいプロセスグループで起動し、取り消しや時間切れではテストが起動した子プロセスごと止める。
func testCommand(ctx context.Context, dir, bin string, flags, env []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, append(slices.Clone(flags), args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	// 止めた後も、孫プロセスが出力の管を握ったまま残ることがあるので、待つ時間に上限を置く。
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
		// TestMain の準備が失敗しても、一覧を得られない。
		return nil, fmt.Errorf("%w: listing tests failed: %w\n%s%s", ErrBaseline, err, out, stderr.Bytes())
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

// coverTest は、変異を無効にしたまま u の i 番目のテストを別のプロセスで実行し、到達と感染を記録する。
func coverTest(ctx context.Context, u *unit, i int, opts Options) (*testCase, error) {
	name := u.names[i]
	path := filepath.Join(u.work, "cover-"+strconv.Itoa(i))
	start := time.Now()
	cmd := testCommand(ctx, u.Pkg.Dir, u.bin, opts.TestFlags, []string{"GAMUT_COVER=" + path, "GAMUT_MUTANT=0"},
		"-test.run", "^"+regexp.QuoteMeta(name)+"$", "-test.count=1", "-test.paniconexit0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s.%s fails, so verdicts would be meaningless: %w\n%s", ErrBaseline, u.Pkg.ImportPath, name, err, out)
	}
	t := &testCase{name: name, duration: time.Since(start), hits: map[int]bool{}, infected: map[int]bool{}}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
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
	return t, nil
}

// selfCheck は、到達の記録があるテストのうち最も短い 1 件を forced fail で実行し、失敗することを確かめる。
// 到達が記録されたのに失敗しないなら、GAMUT_MUTANT がテストのプロセスで読まれていない。
func selfCheck(ctx context.Context, u *unit, opts Options) error {
	dir, bin, work, pkgPath := u.Pkg.Dir, u.bin, u.work, u.Pkg.ImportPath
	var probe *testCase
	for _, t := range u.tests {
		if len(t.hits) > 0 && (probe == nil || t.duration < probe.duration) {
			probe = t
		}
	}
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
	// gamutHit は被覆の記録中にだけ呼ばれるので、記録を有効にして forced fail を渡す。
	env := []string{"GAMUT_COVER=" + filepath.Join(work, "forced-fail"), "GAMUT_MUTANT=" + strconv.Itoa(schemata.ForcedFail)}
	cmd := testCommand(runCtx, dir, bin, opts.TestFlags, env, "-test.run", "^"+regexp.QuoteMeta(probe.name)+"$", "-test.count=1", "-test.paniconexit0")
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
	cmd := testCommand(runCtx, dir, bin, opts.TestFlags, []string{"GAMUT_MUTANT=" + strconv.Itoa(index)},
		"-test.run", "^(?:"+strings.Join(quoted, "|")+")$", "-test.count=1", "-test.failfast", "-test.paniconexit0")
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
	err := buildTest(ctx, pkg, work, name, map[string][]byte{m.File.Path: m.Apply(m.File.Src)}, bin)
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
