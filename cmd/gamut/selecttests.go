package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/diff"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

// 選んだテストの群である。番号の小さい群ほど、変更が壊しうる見込みが高い。
const (
	// groupKiller は、変わった宣言の変異体を検出したテストである。
	groupKiller = 1
	// groupReach は、変わった宣言に到達したほかのテストである。
	groupReach = 2
	// groupChanged は、変わったテストと、変わったテスト用の補助を使うテストである。
	groupChanged = 3
)

// selectedTest は、選んだ 1 件のテストである。
type selectedTest struct {
	Name    string `json:"name"`
	Group   int    `json:"group"`
	Kills   int    `json:"kills"`
	Reaches int    `json:"reaches"`
	Reason  string `json:"reason"`
}

// selectedPackage は、1 つのパッケージの選んだテストで、選んだ順に並ぶ。
type selectedPackage struct {
	Package string          `json:"package"`
	Tests   []*selectedTest `json:"tests"`
}

// selectTestsMain は、差分 changes が壊しうるテストを、キャッシュ store の前回の判定と被覆の記録から選んで出す。何も実行しない。
// キャッシュがないか、環境が変わって読み捨てたときは終了コード 1 にする。
func selectTestsMain(stdout, stderr io.Writer, format string, targets []run.Target, changes *diff.Changes, store *cache.Store) int {
	switch {
	case store.Reset != "":
		fmt.Fprintf(stderr, "gamut: select-tests: the cache cannot be used because %s; run gamut with the same flags first\n", store.Reset)
		return exitError
	case store.Len() == 0:
		fmt.Fprintln(stderr, "gamut: select-tests: the cache has no verdicts; run gamut with the same flags first")
		return exitError
	}
	pkgs := selectTests(targets, changes, store)
	if format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(struct {
			Packages []*selectedPackage `json:"packages"`
		}{pkgs}); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return exitError
		}
		return exitOK
	}
	for _, p := range pkgs {
		names := make([]string, len(p.Tests))
		for i, t := range p.Tests {
			names[i] = regexp.QuoteMeta(t.Name)
		}
		fmt.Fprintf(stdout, "%s\t^(?:%s)$\n", p.Package, strings.Join(names, "|"))
	}
	return exitOK
}

// selectTests は、パッケージごとにテストを選んで並べる。
//
//   - 群 1：変わった宣言の変異体を検出したテストを、検出数の多い順に。kill matrix の判定なら検出したテストのすべてを、そうでなければ最初の 1 件を数える。
//   - 群 2：変わった宣言の変異体に到達したほかのテストを、到達数の多い順に。前回の判定のない宣言（新しい宣言、変異体のない宣言）は、パッケージのテスト全体にする。
//   - 群 3：変わったテストと、変わったテスト用の補助を推移的に参照するテスト。
//
// 前回の判定は、変異体の識別子ではなく、ファイルと宣言の名前で引く。宣言を書き換えると、変異体の識別子は変わりうるためである。
func selectTests(targets []run.Target, changes *diff.Changes, store *cache.Store) []*selectedPackage {
	type declKey struct{ file, decl string }
	entries := map[declKey][]*cache.Entry{}
	store.Each(func(id string, e *cache.Entry) {
		parts := strings.Split(id, "::")
		if len(parts) == 4 {
			k := declKey{parts[0], parts[2]}
			entries[k] = append(entries[k], e)
		}
	})
	var out []*selectedPackage
	for _, t := range targets {
		byPkg := map[string]map[string]*selectedTest{}
		pick := func(name string) *selectedTest {
			pkg := t.Pkg.ImportPath
			// 逆依存パッケージのテストは、<import パス>.<名前> の形で記録されている。
			if i := strings.LastIndex(name, "."); i >= 0 {
				pkg, name = name[:i], name[i+1:]
			}
			if byPkg[pkg] == nil {
				byPkg[pkg] = map[string]*selectedTest{}
			}
			s := byPkg[pkg][name]
			if s == nil {
				s = &selectedTest{Name: name}
				byPkg[pkg][name] = s
			}
			return s
		}
		var fresh []string
		for _, f := range t.Pkg.Files {
			for _, d := range f.AST.Decls {
				start, end := t.Pkg.Fset.Position(d.Pos()).Offset, t.Pkg.Fset.Position(d.End()).Offset
				if !changes.Intersects(f.Path, start, end) {
					continue
				}
				name := mutant.DeclName(d)
				old := entries[declKey{f.Rel, name}]
				if len(old) == 0 {
					fresh = append(fresh, name)
					continue
				}
				for _, e := range old {
					if run.Status(e.Status) == run.Killed {
						killers := e.Killers
						if !e.Matrix && e.Killer != "" {
							killers = []string{e.Killer}
						}
						for _, k := range killers {
							pick(k).Kills++
						}
					}
					for _, r := range e.Reached {
						pick(r).Reaches++
					}
				}
			}
		}
		for _, s := range byPkg {
			for _, sel := range s {
				switch {
				case sel.Kills > 0:
					sel.Group, sel.Reason = groupKiller, fmt.Sprintf("detected %d mutants in the changed declarations", sel.Kills)
				case sel.Reaches > 0:
					sel.Group, sel.Reason = groupReach, fmt.Sprintf("reached %d mutants in the changed declarations", sel.Reaches)
				}
			}
		}
		if len(fresh) > 0 && t.Keys != nil {
			for _, name := range t.Keys.TestFuncs() {
				if s := pick(name); s.Group == 0 {
					s.Group, s.Reason = groupReach, "the package has changed declarations without previous verdicts: "+strings.Join(fresh, ", ")
				}
			}
		}
		if t.Keys != nil {
			funcs := t.Keys.TestFuncs()
			for name := range t.Keys.ChangedTests(changes.Intersects) {
				// 変わった補助そのものはテストではないので選ばない。
				if !slices.Contains(funcs, name) {
					continue
				}
				if s := pick(name); s.Group == 0 {
					s.Group, s.Reason = groupChanged, "the test or a test helper it uses changed"
				}
			}
		}
		for _, pkg := range slices.Sorted(maps.Keys(byPkg)) {
			var tests []*selectedTest
			for _, s := range byPkg[pkg] {
				if s.Group != 0 {
					tests = append(tests, s)
				}
			}
			if len(tests) == 0 {
				continue
			}
			slices.SortFunc(tests, func(a, b *selectedTest) int {
				return cmp.Or(cmp.Compare(a.Group, b.Group), cmp.Compare(b.Kills, a.Kills), cmp.Compare(b.Reaches, a.Reaches), strings.Compare(a.Name, b.Name))
			})
			out = append(out, &selectedPackage{Package: pkg, Tests: tests})
		}
	}
	return out
}
