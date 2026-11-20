// Package run は、schemata のテストバイナリをビルドし、被覆を記録し、変異体を判定する。
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

// Status は判定の状態である。名前は gomutants の報告に合わせる。
type Status string

// 判定の状態。
const (
	Killed     Status = "KILLED"
	Lived      Status = "LIVED"
	NotCovered Status = "NOT COVERED"
	NotViable  Status = "NOT VIABLE"
	TimedOut   Status = "TIMED OUT"
	// Pending は、選択（差分や識別子の指定）の外にあり、判定しなかったことを表す。
	Pending Status = "PENDING"
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
	// Killer は、検出したトップレベルのテストの名前である。KilledBy はサブテストの名前を含みうる。
	Killer  string
	Elapsed time.Duration
	// Cached は、判定を実行せずにキャッシュから再利用したかである。
	Cached bool
	// PreviouslyKilledBy は、前回この変異体を検出したテストが消えたので再検査し、今回は検出されなかったときの、前回の検出テストである。
	PreviouslyKilledBy string
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
	// Cache は判定と被覆の記録の再利用に使うキャッシュである。nil なら再利用も記録もしない。
	// 鍵を持たない Target（Keys が nil）の変異体も再利用しない。
	Cache *cache.Store
	// Select は判定する変異体を絞る。nil なら全変異体を判定する。
	Select *Selection
	// Isolate は、変異体ごとに新しいテストプロセスで判定するかである。偽なら、gamut の実行の間だけ動くテストプロセスで、
	// 複数の変異体を順に判定する（パッケージの初期化を変異体ごとに繰り返さない）。
	Isolate bool

	// rewriteHelper はテスト専用の差し替え口で、補助ファイルの内容をビルドの前に書き換える。
	rewriteHelper func([]byte) []byte
	// afterJob はテスト専用の差し替え口で、変異体の実行が 1 体終わるたびに呼ばれる。
	afterJob func()
	// onExecute はテスト専用の差し替え口で、変異体を実行する前に、テストのプロセスごとのテストの並びを受け取る。
	onExecute func(m *mutant.Mutant, groups [][]string)
	// noBatch はテスト専用の差し替え口で、被覆の記録をまとめずに 1 件ずつ行う。
	noBatch bool
	// onCoverage はテスト専用の差し替え口で、被覆の記録のために実行したテストの名前を受け取る。
	onCoverage func(names []string)
}

// ErrBaseline は、変異を無効にした状態でテストが失敗し、判定の前提が崩れていることを表す。
var ErrBaseline = errors.New("baseline tests fail without mutants")

// ErrSelfCheck は、変異を有効にしてもテストが失敗せず、変異がテストに届いていないと分かったことを表す。
// このまま判定すると、すべての変異体が生き残りになり、テストの弱さと見分けが付かない。
var ErrSelfCheck = errors.New("forced-fail self-check failed")

// Target は、判定する 1 つのパッケージとその変異体である。
type Target struct {
	Pkg     *load.Package
	Mutants []*mutant.Mutant
	// Keys は判定と被覆の記録の再利用の鍵である。nil ならこのパッケージの記録を再利用しない。
	Keys *cache.Keys
	// RevDeps は、変異の検出を試す逆依存パッケージである（統合モード）。
	RevDeps []RevDep
}

// RevDep は、統合モードで変異の検出を試す逆依存パッケージである。
type RevDep struct {
	load.RevDep
	// Key は、cache.RevDepKey によるテストの鍵である。空なら被覆の記録を再利用しない。
	Key string
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
	// overlay は、schemata の書き換えを渡す overlay のファイルである。
	overlay string
	// bins は、変異箇所に到達しうるテストバイナリで、bins[0] がパッケージ自身のものである。
	bins []*binary
	// tests は、すべてのテストバイナリのテストである。
	tests []*testCase
	// buildTime は、パッケージ自身のテストバイナリのビルドにかかった時間である。個別ビルドの見積もりに使う。
	buildTime time.Duration
	// served は、判定のテストから呼べる、パッケージ自身のテストの名前である。nil なら変異体ごとにプロセスを起動する。
	served map[string]bool
	// batches は、逆依存パッケージのテストバイナリをまとめてビルドした回数である。出力先のディレクトリの名前に使う。
	batches int
	// changed は、差分が変えたテストの名前である（--since と --include-test-changes）。
	changed map[string]bool
}

type job struct {
	unit  *unit
	index int
	run   func() *Result
	// mode は判定の方式、reached は変異箇所に到達したテスト、ran は判定のために実行するテストである。
	mode    string
	reached []*testCase
	ran     []*testCase
	// forced は、識別子で指定したのでキャッシュを使わないことを表す。
	forced bool
	// previousKiller は、前回の検出テストが消えたので再検査するときの、前回の検出テストである。
	previousKiller string
	// estimate は実行時間の見積もりである。
	estimate time.Duration
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
		if !opts.Select.touches(u) {
			// 選択に触れないパッケージは、ビルドせずにすべて PENDING にする。
			for k, m := range t.Mutants {
				u.results[k] = &Result{Mutant: m, Status: Pending}
			}
			continue
		}
		viable := 0
		for k, m := range t.Mutants {
			switch {
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
					// テストファイルがなければどのテストも到達しないので、ビルドせずに判定する。
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
	if !opts.Isolate {
		srv = newPool(ctx, opts)
		defer srv.close()
	}
	// 自己検査は被覆の記録の中で行う。記録を戻したテストで検査できるなら、測定と並行させる。
	if err := cover(ctx, active, opts, srv, logf); err != nil {
		return nil, err
	}
	var all []job
	for _, u := range active {
		all = append(all, u.jobs(ctx, opts, srv)...)
	}
	jobs := all
	if opts.Cache != nil {
		jobs = reuse(all, opts.Cache, duplicateIDs(targets))
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
	parallel(len(jobs), opts.Workers, func(k int) {
		j := jobs[k]
		r := j.run()
		// 取り消しの後に終わった実行は、テストが失敗したのか止められたのか区別できないので、記録しない。
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
	logf("execute: %d mutants in %s", len(jobs), time.Since(start).Round(time.Millisecond))

	out := make([][]*Result, len(units))
	for i, u := range units {
		out[i] = u.results
	}
	return out, ctx.Err()
}

// orderJobs は、変異体の実行を見積もり時間の長い順に並べる（LPT 規則）。
// 長い変異体を先に始めると、並列実行の最後に長い変異体が 1 つだけ残って待つことが減る。測定は設計の「実行の順序」に書く。
func orderJobs(jobs []job) {
	slices.SortStableFunc(jobs, func(a, b job) int { return int(b.estimate - a.estimate) })
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

// jobs は、被覆の記録から判定できる変異体の判定を書き、実行が要る変異体の作業を返す。
// 選択の外の変異体は PENDING にする。
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
				if srv != nil && u.canServe(infected) {
					r = u.serve(ctx, m, infected, opts, srv)
				} else {
					r = u.execute(ctx, m, infected, opts, func(b *binary) (string, error) { return b.path, nil })
				}
				r.Mode, r.Tests = ModeSchemata, testNames(reached)
				return r
			}}
		}
		// 個別ビルドで全テストを実行する変異体は、実行するテストを到達したテストとみなして選ぶ。
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
			// 実行せずに決まる判定（到達しない、感染しない）は、どれも検出されていない。
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
