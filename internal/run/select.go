package run

import (
	"math"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/diff"
	"github.com/ambi/gamut/internal/mutant"
)

// Selection は判定する変異体を絞る。Mutants と Changes のどちらか一方を使う。
type Selection struct {
	// Mutants は、識別子で指定した変異体である。nil でなければ、これだけを判定し、その判定にキャッシュを使わない。
	Mutants map[*mutant.Mutant]bool
	// Changes は、--since の差分である。nil でなければ、差分に交わる変異体と、再検査が要る差分の外の変異体だけを判定し、
	// 残りを PENDING にする。
	Changes *diff.Changes
	// DeclScope は、変異体の区間ではなく、囲む宣言の区間と差分の交わりを取るかである。
	DeclScope bool
	// IncludeTestChanges は、差分が変えたテストが到達する、差分の外の生き残りを再検査するかである。
	IncludeTestChanges bool
}

// touches は、パッケージを準備する必要があるかを返す。
// 差分では、差分に交わる変異体があるか、差分がパッケージか逆依存パッケージのテストファイルに触れるときである。
// テストファイルの変更（検出テストの削除を含む）は、差分の外の変異体の再検査を要しうる。
// ソースだけの変更で、交わる変異体がなければ、判定する変異体がないので、ビルドも被覆の記録もしない。
func (s *Selection) touches(u *unit) bool {
	switch {
	case s == nil:
		return true
	case s.Mutants != nil:
		for _, m := range u.Mutants {
			if s.Mutants[m] {
				return true
			}
		}
		return false
	}
	if s.Changes.TouchesTestFile(u.Pkg.Dir) {
		return true
	}
	for _, r := range u.RevDeps {
		if s.Changes.TouchesTestFile(r.Dir) {
			return true
		}
	}
	for _, m := range u.Mutants {
		if s.direct(u, m) {
			return true
		}
	}
	return false
}

// direct は、被覆の記録によらず m を判定するかを返す。
func (s *Selection) direct(u *unit, m *mutant.Mutant) bool {
	switch {
	case s == nil:
		return true
	case s.Mutants != nil:
		return s.Mutants[m]
	}
	node := m.Site
	// RETURN_ZERO の箇所は本体の { だけだが、変異は本体全体を置き換えるので、関数の本体の変更で選ぶ。
	if b, ok := m.Site.(*mutant.BodySite); ok {
		node = b.Func.Body
	}
	if s.DeclScope {
		for _, d := range m.File.AST.Decls {
			if d.Pos() <= m.Site.Pos() && m.Site.End() <= d.End() {
				node = d
			}
		}
	}
	fset := u.Pkg.Fset
	return s.Changes.Intersects(m.File.Path, fset.Position(node.Pos()).Offset, fset.Position(node.End()).Offset)
}

// mayRecheck は、差分の外の変異体を被覆の記録の後に選びうるかを返す。
func (s *Selection) mayRecheck() bool {
	return s != nil && s.Changes != nil
}

// selects は、被覆の記録の後に m を判定するかを返す。tests は変異箇所に到達したテストである。
// 差分の外の変異体は、次のときに再検査する。2 つ目の戻り値は、前回の検出テストが消えたときのその名前である。
//   - 前回 KILLED で、検出したテストが今はない。
//   - IncludeTestChanges のとき、差分が変えたテストが到達し、前回 KILLED でない。
func (s *Selection) selects(u *unit, m *mutant.Mutant, tests []*testCase, store *cache.Store) (bool, string) {
	if s.direct(u, m) {
		return true, ""
	}
	if !s.mayRecheck() {
		return false, ""
	}
	var old *cache.Entry
	if store != nil {
		old = store.Get(m.ID)
	}
	if old != nil && Status(old.Status) == Killed && old.Killer != "" && u.test(old.Killer) == nil {
		return true, old.Killer
	}
	if s.IncludeTestChanges && (old == nil || Status(old.Status) != Killed) {
		for _, t := range tests {
			if u.changed[t.name] {
				return true, ""
			}
		}
	}
	return false, ""
}

// changedTests は、差分が変えたテストの名前を求める。パッケージ自身のテストは、テストファイルの宣言の参照をたどって求め、
// 逆依存パッケージのテストは、テストファイルが変わったらそのパッケージの全テストを変わったとみなす。
func (s *Selection) changedTests(u *unit) map[string]bool {
	out := map[string]bool{}
	if s == nil || s.Changes == nil || !s.IncludeTestChanges {
		return out
	}
	if u.Keys != nil {
		for name := range u.Keys.ChangedTests(s.Changes.Intersects) {
			out[name] = true
		}
	}
	for _, b := range u.bins[1:] {
		touched := false
		for _, f := range b.revdep.TestFiles {
			if s.Changes.Intersects(f, 0, math.MaxInt) {
				touched = true
			}
		}
		if touched {
			for _, t := range b.tests {
				out[t.name] = true
			}
		}
	}
	return out
}
