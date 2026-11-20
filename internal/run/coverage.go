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

// binary は、対象パッケージの書き換えをリンクした 1 つのテストバイナリである。
type binary struct {
	// pkg は、テストを持つパッケージの import パスである。prefix はテストの名前の前に付ける文字列で、パッケージ自身なら空である。
	pkg    string
	prefix string
	dir    string
	path   string
	// revdep は逆依存パッケージのものなら、その情報を持つ。パッケージ自身なら nil である。
	revdep *RevDep
	// key は、逆依存パッケージのテストの鍵である。
	key   string
	names []string
	tests []*testCase
	// lazy は、全テストの被覆の記録を戻せるので、テストの一覧を記録から取り、要るまでビルドしないことを表す。
	lazy bool
	// built は、テストバイナリをビルドしたかである。
	built bool
	// batchable は、1 つのプロセスでまとめて被覆を記録できるテストの、テストバイナリの中での名前である。
	batchable map[string]bool
}

type testCase struct {
	bin *binary
	// name は報告とキャッシュで使う名前で、local はテストバイナリの中での名前である。
	name, local string
	duration    time.Duration
	hits        map[int]bool
	infected    map[int]bool
	// entered は、入った関数の番号（schemata.Result.Funcs の番号）である。
	entered map[int]bool
	// key はテストの鍵である。
	key string
	// measured は、今回の実行で被覆を測ったかである。偽ならキャッシュの記録から戻した。
	measured bool
}

func newTestCase(b *binary, local, key string) *testCase {
	return &testCase{bin: b, name: b.prefix + local, local: local, key: key,
		hits: map[int]bool{}, infected: map[int]bool{}, entered: map[int]bool{}}
}

// test は、名前 name のテストを返す。なければ nil を返す。
func (u *unit) test(name string) *testCase {
	for _, t := range u.tests {
		if t.name == name {
			return t
		}
	}
	return nil
}

// standaloneTests は、到達の記録を持たない個別ビルドの変異体で実行するテストを返す。
// パッケージ自身の全テストと、対象パッケージの変異箇所に到達した逆依存パッケージのテストバイナリの全テストである。
func (u *unit) standaloneTests() []*testCase {
	var out []*testCase
	for i, b := range u.bins {
		if i == 0 || slices.ContainsFunc(b.tests, func(t *testCase) bool { return len(t.hits) > 0 }) {
			out = append(out, b.tests...)
		}
	}
	return out
}

// prepare は、パッケージの schemata を書き換え、テストバイナリをビルドし、テストの一覧を得る。
// 逆依存パッケージのテストバイナリも、同じ書き換えでビルドする。
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
	if !opts.Isolate {
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
		// 全テストの記録を戻せるなら、被覆を測るか変異体を実行するまでビルドしない。
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

// buildOwn は、パッケージ自身のテストバイナリをビルドする。
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

// ownRestorable は、パッケージ自身のテストバイナリの全テストの被覆の記録を戻せるかを返す。
// 戻せるなら、テストの一覧を記録から取る。テストの一覧を得るにはビルドが要るので、
// 記録のテストの集合が、テストファイルのトップレベルのテスト関数の集合と一致し、どのテストの鍵も記録と同じときに限る。
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

// sameTestSet は、記録のテストの集合が、テストファイルのトップレベルのテスト関数の集合と一致するかを返す。
// 一致すれば、テストバイナリを起動して一覧を得る代わりに、それを一覧にする。
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

// build は、逆依存パッケージのテストバイナリ bins を、対象パッケージの書き換えを当ててビルドする。
// パッケージごとに go コマンドを起動すると、依存グラフの読み込みを毎回やり直し、並列度もワーカー数に縛られる。
// そこで go test -c -o <dir>/ に複数のパッケージをまとめて渡し、go コマンドに全 CPU で並列にビルドさせる。
// 出力の名前は import パスの最後の要素で決まるので、同じ名前のパッケージは別の回に分ける。
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
	// 最初の組（最も大きい）で共通の依存をビルドキャッシュに載せる。残りの組はほぼリンクだけなので、CPU の数まで同時に走らせる。
	if err := buildBatch(jobs[0]); err != nil {
		return err
	}
	rest := jobs[1:]
	errs := make([]error, len(rest))
	parallel(len(rest), runtime.NumCPU(), func(i int) { errs[i] = buildBatch(rest[i]) })
	return errors.Join(errs...)
}

// restorable は、逆依存パッケージのテストバイナリ b の全テストの被覆の記録を戻せるかを返す。
// 戻せるなら、テストの一覧を記録から取る。鍵がパッケージごとなので、鍵が同じならテストの一覧も同じである。
// ビルドは、被覆を測るか変異体を実行するときまで遅らせる。どのテストも到達しないパッケージは、ビルドされないままになる。
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

// buildLazy は、tests が使う、まだビルドしていないテストバイナリをビルドする。
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

// writeOverlay は、files を work の下に書き、それらで元のファイルを置き換える overlay のファイルのパスを返す。
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

// buildFlags は、パッケージ importPath のテストバイナリのビルドに足すフラグを返す。どれも判定の結果を変えず、ビルドを短くする。
//   - -vet=off：go test -c は go vet の一部を実行するが、判定には使わない（idmagic の 1 パッケージで約 20%）。
//   - -gcflags=<importPath>=-l：書き換えたパッケージは変異体の数だけ分岐を持ち大きいので、そのパッケージだけインライン化をしない（同じく約 20%）。
//     依存のパッケージのフラグは変えないので、依存はビルドキャッシュのものをそのまま使う。
//   - -ldflags=-s -w：テストバイナリはシンボル表とデバッグ情報（DWARF）を使わないので、リンクを短くする（同じく約 20%）。
//     スタックトレースと runtime.FuncForPC は別の表（pclntab）を使うので、変わらない。
//
// 利用者が GOFLAGS で同じフラグを指定していれば、上書きしないよう足さない。
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

// goTestC は、overlay を当てて dir のパッケージのテストバイナリを bin へビルドする。
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

// cover は、全パッケージのテストの被覆を、キャッシュから戻せるものは戻し、残りを 1 つの作業の列で測り、記録を書き戻す。
// p が nil でなければ、被覆を測ったプロセスを変異体の判定のために p に残す。
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
	// 記録を戻したテストに到達のあるパッケージは、自己検査をいま始め、被覆の測定と重ねる。
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

// coverBatches は、パッケージ自身のテストのうちまとめて測れるものを、ワーカー数の組に分け、組ごとに 1 つのプロセスで実行して、
// テストの関数の先頭に差し込んだ境目で記録を振り分ける。振り分けられなかったテストを返し、それらは 1 件ずつ測る。
// テスト 1 件につき 1 プロセスを起動する代わりに、組の数だけ起動する。
// p が nil でなく、組のテストをすべて判定のテストから呼べるなら、組を判定のテストのプロセスで測り、そのプロセスを判定に使い回す。
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

// coverChunk は、tests を 1 つのプロセスで -test.parallel=1 で実行し、記録を境目 "t <名前>" で振り分ける。
// プロセスが失敗したか、境目のないテストがあれば偽を返し、tests の記録を空に戻す。
func coverChunk(ctx context.Context, tests []*testCase, path string, opts Options) bool {
	b := tests[0].bin
	quoted := make([]string, len(tests))
	for i, t := range tests {
		quoted[i] = regexp.QuoteMeta(t.local)
	}
	start := time.Now()
	cmd := testCommand(ctx, b.dir, b.path, opts.TestFlags, []string{"GAMUT_COVER=" + path, "GAMUT_MUTANT=0"},
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

// coverServed は、tests を判定のテストのプロセスで変異なしで実行して被覆を記録し、記録を境目で振り分ける。
// 成功したら、そのプロセスは被覆の記録を止めて、変異体の判定のために p に戻る。テストの変数の初期化を、被覆と判定で 1 回にする。
// 失敗したら、プロセスを捨てて偽を返す。
func coverServed(ctx context.Context, tests []*testCase, path string, opts Options, p *pool) bool {
	b := tests[0].bin
	start := time.Now()
	s, err := startServer(ctx, b, opts, []string{"GAMUT_COVER=" + path})
	if err != nil {
		return false
	}
	outcome, durations := s.cover(ctx, testNames(tests))
	if outcome != servedPass {
		s.close()
		return false
	}
	if !splitRecords(tests, path, durations, time.Since(start)) {
		s.close()
		return false
	}
	p.put(b, s)
	return true
}

// splitRecords は、path の記録を境目 "t <名前>" で tests に振り分け、所要時間を durations から（なければ elapsed を等分して）入れる。
// 最初の境目より前の記録（パッケージの初期化）は、どのテストにも現れるものとして全テストに足す。
// 境目のないテストがあれば偽を返し、tests の記録を空に戻す。
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
		}
	}
	if cur == nil || len(seen) != len(tests) {
		for _, t := range tests {
			t.hits, t.infected, t.entered = map[int]bool{}, map[int]bool{}, map[int]bool{}
		}
		return false
	}
	for _, t := range tests {
		maps.Copy(t.hits, common.hits)
		maps.Copy(t.infected, common.infected)
		maps.Copy(t.entered, common.entered)
		t.duration = durations[t.local]
		if t.duration == 0 {
			t.duration = elapsed / time.Duration(len(tests))
		}
	}
	return true
}

// probeRevDeps は、逆依存パッケージのテストバイナリごとに、測るテストをまとめて 1 つのプロセスで実行し、
// 変異箇所への到達も関数への入りも記録されなければ、それらのテストを空の記録として 1 件ずつの測定から外す。
// 逆依存パッケージの多くは対象パッケージの変異箇所に到達しないので、テストの数だけのプロセスの起動を 1 回にできる。
// まとめた実行が失敗したら、1 件ずつ測る（基準のテストの失敗は、そこでテストごとに報告する）。
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
		cmd := testCommand(ctx, b.dir, b.path, opts.TestFlags, []string{"GAMUT_COVER=" + path, "GAMUT_MUTANT=0"},
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

// dirWork は、被覆の記録のファイルを置くディレクトリを返す。
func (b *binary) dirWork() string { return filepath.Dir(b.path) }

// plan は、u のすべてのテストを作り、キャッシュの記録を戻せるテストは戻し、測り直すテストに measured を立てて返す。
//
// 記録を戻すのは、次をすべて満たすテストである。
//   - 記録したときと、パッケージの外の依存、初期化の宣言、全変異箇所の書き換えの段階が同じである。
//   - テストの鍵が同じである。
//   - 記録にある、入った関数と到達した変異箇所を囲む宣言が、どれも今もそのままある（依存を含めて変わらない）。
//   - 記録にある変異箇所と変異体が今もある。
//
// 識別子で変異体を指定したときは、指定した変異体の変異箇所に到達していたテストも測り直す。
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

// restorer は、キャッシュの被覆の記録を今の書き換えの番号へ写す。
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
			// 同じ鍵の箇所が 2 つあれば（同じ形の init が 2 つあるときなど）、記録を区別できない。
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

// saveCoverage は、今回のすべてのテストの被覆をキャッシュへ書く。戻したテストの記録は、戻したものと同じ内容で書き直す。
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
			rec := &cache.TestRecord{Key: t.key, Duration: int64(t.duration)}
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
			rec.Reach = slices.Sorted(maps.Keys(reach))
			slices.Sort(rec.Hits)
			slices.Sort(rec.Infected)
			cov.Tests[t.local] = rec
		}
		store.PutCoverage(u.Pkg.ImportPath, b.pkg, cov)
	}
}

// coverTest は、変異を無効にしたまま t を別のプロセスで実行し、到達、感染、入った関数を記録する。
func coverTest(ctx context.Context, t *testCase, path string, opts Options) error {
	b := t.bin
	start := time.Now()
	cmd := testCommand(ctx, b.dir, b.path, opts.TestFlags, []string{"GAMUT_COVER=" + path, "GAMUT_MUTANT=0"},
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
		}
	}
	return nil
}

// selfCheck は、パッケージ自身のテストのうち到達の記録がある最も短い 1 件を forced fail で実行し、失敗することを確かめる。
// 到達が記録されたのに失敗しないなら、GAMUT_MUTANT がテストのプロセスで読まれていない。
func selfCheck(ctx context.Context, u *unit, opts Options) error {
	if !u.bins[0].built {
		// ビルドしなかったのは、被覆を測るテストも実行する変異体もないときである。
		// 判定はすべて、同じ環境で自己検査を通った前回の実行の記録から来る。
		return nil
	}
	return selfCheckWith(ctx, u, shortestReaching(u.bins[0].tests, func(*testCase) bool { return true }), opts)
}

// shortestReaching は、tests のうち ok を満たし、到達の記録がある最も短いテストを返す。
func shortestReaching(tests []*testCase, ok func(*testCase) bool) *testCase {
	var probe *testCase
	for _, t := range tests {
		if ok(t) && len(t.hits) > 0 && (probe == nil || t.duration < probe.duration) {
			probe = t
		}
	}
	return probe
}

// selfCheckWith は、probe を forced fail で実行し、失敗することを確かめる。probe が nil なら、検査を飛ばしたことを警告する。
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
	// gamutHit は被覆の記録中にだけ呼ばれるので、記録を有効にして forced fail を渡す。
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
