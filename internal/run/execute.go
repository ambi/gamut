package run

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/mutant"
)

var failLine = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)

// priorKillers は、m の実行で先に試すテストの名前を、試す順に返す。
// 前回 m を検出したテストを先頭に、同じ変異箇所の別の変異体を検出したテストを続ける。
// キャッシュは実行が終わるたびに書かれるので、今回の実行で先に検出されたものも含む。
func (u *unit) priorKillers(m *mutant.Mutant, store *cache.Store) []string {
	if store == nil {
		return nil
	}
	var out []string
	add := func(id string) {
		if e := store.Get(id); e != nil && Status(e.Status) == Killed && e.Killer != "" && !slices.Contains(out, e.Killer) {
			out = append(out, e.Killer)
		}
	}
	add(m.ID)
	if s := u.sch.SiteOf[m]; s != nil {
		for _, other := range s.Mutants {
			if other != m {
				add(other.ID)
			}
		}
	}
	return out
}

// phases は、tests を、テストのプロセスごとのまとまりに分けて、実行する順に返す。
// 1 つ目は前回の検出テスト（prior[0]）だけ、2 つ目は残りの prior、最後に残りのテストである。
// 1 つのプロセスの中ではテストは宣言の順に走るので、-test.run の並びでは順序を決められない。そのためプロセスを分ける。
// どのまとまりも 1 つのテストバイナリのテストだけを持ち、パッケージ自身のテストバイナリを先にする。
func phases(tests []*testCase, prior []string, bins []*binary) [][]*testCase {
	byName := map[string]*testCase{}
	for _, t := range tests {
		byName[t.name] = t
	}
	used := map[*testCase]bool{}
	var out [][]*testCase
	group := func(ts []*testCase) {
		for _, b := range bins {
			var g []*testCase
			for _, t := range ts {
				if t.bin == b && !used[t] {
					g = append(g, t)
				}
			}
			for _, t := range g {
				used[t] = true
			}
			if len(g) > 0 {
				out = append(out, g)
			}
		}
	}
	var first, rest []*testCase
	for i, name := range prior {
		if t := byName[name]; t != nil {
			if i == 0 {
				first = append(first, t)
			} else {
				rest = append(rest, t)
			}
		}
	}
	group(first)
	group(rest)
	group(tests)
	return out
}

// execute は、変異体 m を有効にして tests を実行する。前回の検出テストを先に、別のプロセスで実行し、最初の失敗で止める。
// binPath は、テストバイナリごとに実行するファイルを返す。誤りを返したら、変異体を構築不能とする。
func (u *unit) execute(ctx context.Context, m *mutant.Mutant, tests []*testCase, opts Options, binPath func(*binary) (string, error)) *Result {
	groups := phases(tests, u.priorKillers(m, opts.Cache), u.bins)
	if opts.onExecute != nil {
		names := make([][]string, len(groups))
		for i, g := range groups {
			names[i] = testNames(g)
		}
		opts.onExecute(m, names)
	}
	index := m.Index
	start := time.Now()
	r := &Result{Mutant: m, Status: Lived, Reason: ReasonNotDetected}
	for _, g := range groups {
		b := g[0].bin
		path, err := binPath(b)
		if err != nil {
			return &Result{Mutant: m, Status: NotViable, Elapsed: time.Since(start)}
		}
		if path != b.path {
			// 個別ビルドのバイナリは変異を直接書き込んでいるので、選択の番号を渡さない。
			index = 0
		}
		quoted := make([]string, len(g))
		for i, t := range g {
			quoted[i] = regexp.QuoteMeta(t.local)
		}
		limit := time.Duration(float64(total(g))*opts.TimeoutFactor) + opts.TimeoutMin
		runCtx, cancel := context.WithTimeout(ctx, limit)
		cmd := testCommand(runCtx, b.dir, path, opts.TestFlags, []string{"GAMUT_MUTANT=" + strconv.Itoa(index)},
			"-test.run", "^(?:"+strings.Join(quoted, "|")+")$", "-test.count=1", "-test.failfast", "-test.paniconexit0")
		out, err := cmd.CombinedOutput()
		timedOut := runCtx.Err() != nil && ctx.Err() == nil
		cancel()
		switch {
		case timedOut:
			r.Status, r.Reason = TimedOut, ""
		case err != nil:
			r.Status, r.Reason = Killed, ""
			if f := failLine.FindSubmatch(out); f != nil {
				r.KilledBy = b.prefix + string(f[1])
				top, _, _ := strings.Cut(string(f[1]), "/")
				r.Killer = b.prefix + top
			} else if len(g) == 1 {
				// 失敗の行がなくても（os.Exit など）、1 件だけ実行したならそのテストが検出した。
				r.Killer = g[0].name
			}
		default:
			continue
		}
		break
	}
	r.Elapsed = time.Since(start)
	return r
}

// standalone は、schemata に埋め込めなかった変異体を、テストバイナリごとに個別にビルドして判定する。
// ビルドは、そのテストバイナリのテストを実行する番が来たときに行う。
func (u *unit) standalone(ctx context.Context, m *mutant.Mutant, tests []*testCase, opts Options) *Result {
	name := "standalone-" + strconv.Itoa(m.Index)
	overlay, err := writeOverlay(u.work, name, map[string][]byte{m.File.Path: m.Apply(m.File.Src)})
	if err != nil {
		return &Result{Mutant: m, Status: NotViable, Mode: ModeStandalone}
	}
	var built []string
	defer func() {
		for _, p := range built {
			_ = os.Remove(p)
		}
	}()
	paths := map[*binary]string{}
	r := u.execute(ctx, m, tests, opts, func(b *binary) (string, error) {
		if p, ok := paths[b]; ok {
			return p, nil
		}
		p := filepath.Join(u.work, name+"-"+strconv.Itoa(slices.Index(u.bins, b))+".test")
		// 型検査を通ってもコンパイラが拒む変異体（定数の桁あふれなど）は構築不能とする。
		if err := goTestC(ctx, b.dir, u.Pkg.ImportPath, u.Pkg.Tags, overlay, p); err != nil {
			return "", err
		}
		built = append(built, p)
		paths[b] = p
		return p, nil
	})
	r.Mode = ModeStandalone
	return r
}
