package run

import (
	"bytes"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ambi/gamut/internal/mutant"
)

// IgnoreSelection は、補助ファイルが GAMUT_MUTANT を読まないように壊した書き換えで実行する設定を返す。
// 変異がテストに届かない不具合を再現し、自己検査がそれを見つけることを確かめるためにだけ使う。
func IgnoreSelection(o Options) Options {
	o.rewriteHelper = func(src []byte) []byte {
		return bytes.Replace(src, []byte(`var gamutActive = gamutEnvInt("GAMUT_MUTANT")`), []byte(`var gamutActive = 0`), 1)
	}
	return o
}

// AfterJob は、変異体の実行が 1 体終わるたびに fn を呼ぶ設定を返す。実行の途中で取り消すテストに使う。
func AfterJob(o Options, fn func()) Options {
	o.afterJob = fn
	return o
}

// OnExecute は、変異体を実行する前に、テストのプロセスごとのテストの並びを fn に渡す設定を返す。
func OnExecute(o Options, fn func(id string, groups [][]string)) Options {
	o.onExecute = func(m *mutant.Mutant, groups [][]string) { fn(m.ID, groups) }
	return o
}

// OnCoverage は、被覆の記録のために実行したテストの名前を fn に渡す設定を返す。
func OnCoverage(o Options, fn func(names []string)) Options {
	o.onCoverage = fn
	return o
}

// Phases は、テストの名前と時間から、前回の検出テスト prior を先にしたプロセスごとの並びを返す。
// 名前に "." を含むテストは、その前の部分を import パスとする逆依存パッケージのテストとして扱う。
func Phases(tests map[string]time.Duration, prior []string) [][]string {
	own := &binary{pkg: "own"}
	bins := []*binary{own}
	byPkg := map[string]*binary{}
	var ts []*testCase
	for _, name := range slices.Sorted(maps.Keys(tests)) {
		b := own
		if pkg, local, ok := strings.Cut(name, "."); ok {
			if byPkg[pkg] == nil {
				byPkg[pkg] = &binary{pkg: pkg, prefix: pkg + "."}
				bins = append(bins, byPkg[pkg])
			}
			b = byPkg[pkg]
			name = local
		}
		t := newTestCase(b, name, "")
		t.duration = tests[b.prefix+name]
		ts = append(ts, t)
	}
	var out [][]string
	for _, g := range phases(ts, prior, bins) {
		out = append(out, testNames(g))
	}
	return out
}

// NoBatch は、被覆の記録を 1 件ずつのプロセスだけで行う設定を返す。まとめた記録と比べるために使う。
func NoBatch(o Options) Options {
	o.noBatch = true
	return o
}
