// Package schemata は、構築可能な変異体をすべて埋め込んだソースを作る。
// 変異を無効にした状態では元のソースと同じに振る舞い、環境変数 GAMUT_MUTANT で 1 体を有効にする。
package schemata

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
)

// HelperFileName は、選択と被覆の記録を置く補助ファイルの名前である。
const HelperFileName = "gamut_schemata.go"

// TestStartName は、テストの関数の先頭から呼ぶ、記録の境目を書く補助関数の名前である。
const TestStartName = "GamutTestStart"

// ForcedFail は、GAMUT_MUTANT に渡すと、到達した変異箇所で panic させる値である。
// 変異の選択がテストに届くことを、判定の前に確かめるために使う。
const ForcedFail = -1

// helperNames は補助ファイルが宣言する名前である。対象パッケージと衝突してはならない。
var helperNames = []string{"gamutActive", "gamutCovering", "gamutHits", "gamutInfected", "gamutMu", "gamutOut", "gamutEnvInt", "gamutHit", "gamutInfect", "gamutRecord", "gamutMark", "gamutEntered", "gamutEnter", "gamutOpen", TestStartName, "GamutSetActive", "GamutCoverStop"}

// Level は、変異箇所をどこまで書き換えるかである。
type Level int

const (
	// Wrapped は、変異体を埋め込み、到達と感染を記録する。
	Wrapped Level = iota
	// Marked は、変異体を埋め込めないが、値をそのまま返す gamutMark で包んで到達を記録する。
	// 変異体は個別にビルドし、到達したテストだけで判定する。
	Marked
	// Untouched は書き換えない。変異体は個別にビルドし、全テストで判定する。
	Untouched
)

// Site は 1 つの変異箇所（ノード）と、そこに埋め込む変異体である。
type Site struct {
	// Index は被覆の記録で使う番号で、1 から始まる。Untouched なら 0 である。
	Index   int
	Level   Level
	Node    ast.Node
	File    *load.File
	Mutants []*mutant.Mutant

	start, end int
}

func (s *Site) next() Level {
	switch s.Node.(type) {
	case *ast.IncDecStmt, *ast.AssignStmt, *ast.BranchStmt, *ast.ExprStmt, *mutant.BodySite:
		// 文は値を返さないので gamutMark で包めない。
		return Untouched
	}
	return s.Level + 1
}

// Result は書き換えの結果である。
type Result struct {
	// Files は、元のファイルの絶対パスから書き換え後の内容への対応である。
	Files map[string][]byte
	// HelperPath と Helper は、overlay で足す補助ファイルのパスと内容である。
	HelperPath string
	Helper     []byte
	// Sites は構築可能な変異体を持つすべての変異箇所である。
	Sites []*Site
	// SiteOf は、構築可能な変異体からその変異箇所への対応である。
	SiteOf map[*mutant.Mutant]*Site
	// Funcs は、被覆の記録で入ったことを記録する関数の宣言である。Funcs[i] の番号は i+1 である。
	Funcs []*ast.FuncDecl
}

// Count は、Level ごとの変異体の数を返す。
func (r *Result) Count(level Level) int {
	n := 0
	for _, s := range r.Sites {
		if s.Level == level {
			n += len(s.Mutants)
		}
	}
	return n
}

// Build は viable な変異体を埋め込んだソースを作る。
// 書き換え後のパッケージが型検査を通らない箇所は、通るまで個別ビルドへ回す。
func Build(pkg *load.Package, mutants []*mutant.Mutant) (*Result, error) {
	helperPath := filepath.Join(pkg.Dir, HelperFileName)
	for _, f := range pkg.Files {
		if f.Path == helperPath {
			return nil, fmt.Errorf("%s already exists", helperPath)
		}
	}
	for _, name := range helperNames {
		if pkg.Types.Scope().Lookup(name) != nil {
			return nil, fmt.Errorf("package %s already declares %s, which gamut needs", pkg.ImportPath, name)
		}
	}

	bySite := map[ast.Node]*Site{}
	var sites []*Site
	for _, m := range mutants {
		if !m.Viable {
			continue
		}
		s, ok := bySite[m.Site]
		if !ok {
			s = &Site{
				Node:  m.Site,
				File:  m.File,
				start: pkg.Fset.Position(m.Site.Pos()).Offset,
				end:   pkg.Fset.Position(m.Site.End()).Offset,
			}
			bySite[m.Site] = s
			sites = append(sites, s)
		}
		s.Mutants = append(s.Mutants, m)
	}

	funcs, entries := entryPoints(pkg)

	// 各反復で 1 つ以上の箇所の Level が上がるので、反復は箇所の数の 2 倍で終わる。
	for range 2*len(sites) + 1 {
		indexed := 0
		for _, s := range sites {
			s.Index = 0
			if s.Level != Untouched {
				indexed++
				s.Index = indexed
			}
		}
		r := &renderer{pkg: pkg, entries: entries}
		files := map[string][]byte{}
		for _, f := range pkg.Files {
			var inFile []*Site
			for _, s := range sites {
				if s.File == f && s.Level != Untouched {
					inFile = append(inFile, s)
				}
			}
			if len(inFile) == 0 && len(entries[f.Path]) == 0 {
				continue
			}
			files[f.Path] = r.renderFile(f, inFile)
		}
		// 描画中に書き換えられないと分かった箇所があれば、Level を上げて描き直す。
		if len(r.demotions) > 0 {
			for s, level := range r.demotions {
				s.Level = max(s.Level, level)
			}
			continue
		}

		helper := renderHelper(pkg.Name, indexed, len(mutants), len(funcs))
		bad, err := typeErrors(pkg, files, helperPath, helper, r.ranges)
		if err != nil {
			return nil, err
		}
		if len(bad) > 0 {
			for _, s := range bad {
				s.Level = s.next()
			}
			continue
		}

		siteOf := map[*mutant.Mutant]*Site{}
		for _, s := range sites {
			for _, m := range s.Mutants {
				siteOf[m] = s
			}
		}
		return &Result{Files: files, HelperPath: helperPath, Helper: helper, Sites: sites, SiteOf: siteOf, Funcs: funcs}, nil
	}
	return nil, fmt.Errorf("schemata rewrite of %s did not converge", pkg.ImportPath)
}

// entry は、関数の本文の { の直後に差し込む、入ったことの記録である。
type entry struct {
	offset int
	text   string
}

// entryPoints は、対象ファイルの本文を持つ関数の宣言に番号を振り、ファイルごとの差し込み位置を返す。
// 被覆の記録は、変異箇所に到達しなくても、テストが入った関数を知るためにこれを使う。
// //go:nosplit の関数は、呼び出しを足すとスタックの上限を超えうるので除く。
func entryPoints(pkg *load.Package) ([]*ast.FuncDecl, map[string][]entry) {
	var funcs []*ast.FuncDecl
	entries := map[string][]entry{}
	for _, f := range pkg.Files {
		for _, d := range f.AST.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || nosplit(fd) {
				continue
			}
			funcs = append(funcs, fd)
			entries[f.Path] = append(entries[f.Path], entry{
				offset: pkg.Fset.Position(fd.Body.Lbrace).Offset + 1,
				text:   fmt.Sprintf(" if gamutCovering { gamutEnter(%d) };", len(funcs)),
			})
		}
	}
	return funcs, entries
}

func nosplit(fd *ast.FuncDecl) bool {
	if fd.Doc == nil {
		return false
	}
	for _, c := range fd.Doc.List {
		if strings.HasPrefix(c.Text, "//go:nosplit") {
			return true
		}
	}
	return false
}

type outRange struct {
	site       *Site
	start, end int
}

type renderer struct {
	pkg *load.Package
	// entries は、ファイルごとの関数の入口の差し込みである。変異箇所の外の字句を写すときに差し込む。
	entries map[string][]entry
	// demotions は、描画中に書き換えられないと分かった箇所と、次に試す Level である。
	demotions map[*Site]Level
	// ranges は、ファイルごとに各箇所が書き換え後の内容のどこを占めるかである。
	ranges map[string][]outRange
	// top は、次の renderRange がファイルの最上位の呼び出しであるかである。
	top bool
}

func (r *renderer) demote(s *Site, level Level) {
	if r.demotions == nil {
		r.demotions = map[*Site]Level{}
	}
	r.demotions[s] = max(r.demotions[s], level)
}

func (r *renderer) renderFile(f *load.File, sites []*Site) []byte {
	slices.SortFunc(sites, func(a, b *Site) int {
		if a.start != b.start {
			return a.start - b.start
		}
		if a.end != b.end {
			return b.end - a.end
		}
		// 条件を包む箇所は、包まれる式の箇所と範囲が同じなので先にする。包まれる式はその中で書き換える。
		_, ca := a.Node.(*mutant.ConditionSite)
		_, cb := b.Node.(*mutant.ConditionSite)
		switch {
		case ca && !cb:
			return -1
		case cb && !ca:
			return 1
		}
		return 0
	})
	if r.ranges == nil {
		r.ranges = map[string][]outRange{}
	}
	var buf bytes.Buffer
	r.top = true
	r.renderRange(&buf, f, sites, 0, len(f.Src))
	r.top = false
	return buf.Bytes()
}

// copySrc は、元のソースの [lo, hi) を buf へ写す。ファイルの最上位の字句なら、関数の入口の記録を差し込む。
// 関数の宣言の本文の { は変異箇所の式の中に現れないので、最上位で写す字句だけを見ればよい。
func (r *renderer) copySrc(buf *bytes.Buffer, f *load.File, lo, hi int, top bool) {
	if top {
		for _, e := range r.entries[f.Path] {
			if lo < e.offset && e.offset <= hi {
				buf.Write(f.Src[lo:e.offset])
				buf.WriteString(e.text)
				lo = e.offset
			}
		}
	}
	buf.Write(f.Src[lo:hi])
}

// renderRange は元のソースの [lo, hi) を buf へ書き、その中の変異箇所を書き換える。
func (r *renderer) renderRange(buf *bytes.Buffer, f *load.File, sites []*Site, lo, hi int) {
	top := r.top
	r.top = false
	defer func() { r.top = top }()
	pos := lo
	for _, s := range sites {
		if s.start < pos || s.end > hi {
			continue
		}
		r.copySrc(buf, f, pos, s.start, top)
		start := buf.Len()
		if s.Level == Marked {
			fmt.Fprintf(buf, "gamutMark(%d, ", s.Index)
			r.renderRange(buf, f, without(sites, s), s.start, s.end)
			buf.WriteString(")")
		} else {
			r.renderSite(buf, f, sites, s)
		}
		r.ranges[f.Path] = append(r.ranges[f.Path], outRange{site: s, start: start, end: buf.Len()})
		pos = s.end
	}
	r.copySrc(buf, f, pos, hi, top)
}

func without(sites []*Site, s *Site) []*Site {
	out := make([]*Site, 0, len(sites))
	for _, x := range sites {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// demoteWithin は、[lo, hi) の中の箇所を書き換えない。本文へそのまま写す字句の中の箇所が対象である。
func (r *renderer) demoteWithin(sites []*Site, lo, hi int) {
	for _, s := range sites {
		if s.start >= lo && s.end <= hi {
			r.demote(s, Untouched)
		}
	}
}

func (r *renderer) offset(p token.Pos) int { return r.pkg.Fset.Position(p).Offset }

func (r *renderer) text(f *load.File, n ast.Node) string {
	return string(f.Src[r.offset(n.Pos()):r.offset(n.End())])
}

// operand は、関数リテラルの引数として渡すか、本文へ直接写す被演算子である。
type operand struct {
	expr   ast.Expr
	inline bool
	// ref は本文での参照（引数名または写した字句）である。
	ref string
	// typ は引数として渡すときの型の字句である。
	typ string
}

func (r *renderer) operand(f *load.File, sites []*Site, e ast.Expr, param string) (operand, bool) {
	tv := r.pkg.Info.Types[e]
	if tv.Value != nil || tv.IsNil() || pure(e) {
		// 定数、nil、副作用のない参照は本文へ写す。型を書かずに済み、何度評価しても結果は同じである。
		r.demoteWithin(sites, r.offset(e.Pos()), r.offset(e.End()))
		return operand{expr: e, inline: true, ref: "(" + r.text(f, e) + ")"}, true
	}
	typ, ok := r.typeString(f, tv.Type)
	if !ok {
		return operand{}, false
	}
	return operand{expr: e, ref: param, typ: typ}, true
}

// pure は、e が関数呼び出しも変異箇所も含まない参照であるかを返す。
// そのような式は何度評価しても同じ値になり、panic するなら元の式も同じ所で panic する。
func pure(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident, *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return pure(x.X)
	case *ast.SelectorExpr:
		return pure(x.X)
	case *ast.StarExpr:
		return pure(x.X)
	case *ast.IndexExpr:
		return pure(x.X) && pure(x.Index)
	}
	return false
}

func (r *renderer) renderSite(buf *bytes.Buffer, f *load.File, sites []*Site, s *Site) {
	fallback := func() {
		r.demote(s, s.next())
		buf.Write(f.Src[s.start:s.end])
	}
	switch n := s.Node.(type) {
	case *ast.BinaryExpr:
		if n.Op == token.LAND || n.Op == token.LOR {
			r.renderLogical(buf, f, sites, s, n, fallback)
			return
		}
		resultType, ok := r.resultType(f, n)
		if !ok {
			fallback()
			return
		}
		x, okx := r.operand(f, sites, n.X, "gamutL")
		y, oky := r.operand(f, sites, n.Y, "gamutR")
		if !okx || !oky {
			fallback()
			return
		}
		expr := func(op token.Token) string { return x.ref + " " + op.String() + " " + y.ref }
		var body strings.Builder
		fmt.Fprintf(&body, "if gamutCovering { gamutHit(%d); ", s.Index)
		integer := isInteger(r.pkg.Info.Types[n].Type)
		for _, m := range s.Mutants {
			cond := fmt.Sprintf("(%s) != (%s)", expr(m.Replacement), expr(n.Op))
			if m.Replacement == token.QUO && integer {
				// 整数の 0 除算は panic するので、比べずに値が変わったとみなす。
				cond = fmt.Sprintf("%s == 0 || %s", y.ref, cond)
			}
			fmt.Fprintf(&body, "if %s { gamutInfect(%d) }; ", cond, m.Index)
		}
		body.WriteString("}; switch gamutActive { ")
		for _, m := range s.Mutants {
			fmt.Fprintf(&body, "case %d: return %s; ", m.Index, expr(m.Replacement))
		}
		fmt.Fprintf(&body, "}; return %s ", expr(n.Op))

		var params []string
		var args []operand
		for _, o := range []operand{x, y} {
			if !o.inline {
				params = append(params, o.ref+" "+o.typ)
				args = append(args, o)
			}
		}
		fmt.Fprintf(buf, "func(%s) %s { %s}(", strings.Join(params, ", "), resultType, body.String())
		r.renderArgs(buf, f, sites, args)
		buf.WriteString(")")

	case *ast.UnaryExpr:
		resultType, ok := r.resultType(f, n)
		if !ok {
			fallback()
			return
		}
		x, okx := r.operand(f, sites, n.X, "gamutX")
		if !okx {
			fallback()
			return
		}
		var body strings.Builder
		fmt.Fprintf(&body, "if gamutCovering { gamutHit(%d); ", s.Index)
		for _, m := range s.Mutants {
			fmt.Fprintf(&body, "if (%s%s) != (%s%s) { gamutInfect(%d) }; ", m.ReplacementText, x.ref, n.Op, x.ref, m.Index)
		}
		body.WriteString("}; switch gamutActive { ")
		for _, m := range s.Mutants {
			// REMOVE_LOGICAL_NOT の置き換え後の文字列は空なので、被演算子だけを返す。
			fmt.Fprintf(&body, "case %d: return %s%s; ", m.Index, m.ReplacementText, x.ref)
		}
		fmt.Fprintf(&body, "}; return %s%s ", n.Op, x.ref)
		if x.inline {
			fmt.Fprintf(buf, "func() %s { %s}()", resultType, body.String())
			return
		}
		fmt.Fprintf(buf, "func(%s %s) %s { %s}(", x.ref, x.typ, resultType, body.String())
		r.renderArgs(buf, f, sites, []operand{x})
		buf.WriteString(")")

	case *ast.IncDecStmt:
		// 対象の式は本文に 2 回写すので、その中の箇所は個別ビルドへ回す。
		r.demoteWithin(sites, r.offset(n.X.Pos()), r.offset(n.X.End()))
		target := r.text(f, n.X)
		m := s.Mutants[0]
		fmt.Fprintf(buf, "func() { if gamutCovering { gamutHit(%d); gamutInfect(%d) }; if gamutActive == %d { %s%s } else { %s%s } }()",
			s.Index, m.Index, m.Index, target, m.Replacement, target, n.Tok)

	case *mutant.BodySite:
		// RETURN_ZERO：本体の { の直後で、有効な変異体ならゼロ値を返す。関数に入るたびに到達し、返す値は変わるので感染したとする。
		// 関数の入口の記録も { の直後に差し込むが、この箇所が { を書き換えるので、ここで書く。
		m := s.Mutants[0]
		zeros := strings.TrimSuffix(strings.TrimPrefix(m.ReplacementText, "{ return "), ";")
		buf.WriteString("{")
		for _, e := range r.entries[f.Path] {
			if e.offset == s.end {
				buf.WriteString(e.text)
			}
		}
		fmt.Fprintf(buf, " if gamutCovering { gamutHit(%d); gamutInfect(%d) }; if gamutActive == %d { return %s };", s.Index, m.Index, m.Index, zeros)

	case *ast.ExprStmt:
		// STATEMENT_REMOVE：有効な変異体なら呼び出しの文を実行しない。引数の中の変異箇所は、実行する側の中で書き換える。
		// 文を 2 つの文にするので、for の後処理のような単純な文しか書けない所では、書き換え後の型検査で個別ビルドに回る。
		m := s.Mutants[0]
		fmt.Fprintf(buf, "if gamutCovering { gamutHit(%d); gamutInfect(%d) }; if gamutActive != %d { ", s.Index, m.Index, m.Index)
		r.renderRange(buf, f, without(sites, s), s.start, s.end)
		buf.WriteString(" }")

	case *mutant.ConditionSite:
		// CONDITIONS_CONSTANT：有効な変異体なら条件を評価せずに定数を返し、そうでなければ条件を評価する。
		// 条件の中の変異箇所は、評価する側の中でそのまま書き換える。
		resultType, ok := r.resultType(f, n.X)
		if !ok {
			fallback()
			return
		}
		fmt.Fprintf(buf, "func() %s { switch gamutActive { ", resultType)
		for _, m := range s.Mutants {
			fmt.Fprintf(buf, "case %d: return %s; ", m.Index, m.ReplacementText)
		}
		buf.WriteString("}; gamutC := ")
		r.renderRange(buf, f, without(sites, s), s.start, s.end)
		fmt.Fprintf(buf, "; if gamutCovering { gamutHit(%d); ", s.Index)
		for _, m := range s.Mutants {
			fmt.Fprintf(buf, "if gamutC != %s { gamutInfect(%d) }; ", m.ReplacementText, m.Index)
		}
		buf.WriteString("}; return gamutC }()")

	case *ast.Ident:
		// BOOLEAN_LITERAL：true か false を、その場の型を返す関数呼び出しにする。値は必ず変わるので、到達したら感染したとする。
		// const の宣言の中など、呼び出しを書けない文脈は、書き換え後の型検査で個別ビルドへ回る。
		resultType, ok := r.resultType(f, n)
		if !ok {
			fallback()
			return
		}
		m := s.Mutants[0]
		fmt.Fprintf(buf, "func() %s { if gamutCovering { gamutHit(%d); gamutInfect(%d) }; if gamutActive == %d { return %s }; return %s }()",
			resultType, s.Index, m.Index, m.Index, m.ReplacementText, n.Name)

	case *ast.BranchStmt:
		// break と continue は同じ関数の中のループに作用するので、関数リテラルで包めない。文を 2 つの分岐にする。
		// 分岐にしたことでループの終端性が変わり、missing return になれば、書き換え後の型検査で個別ビルドへ回る。
		label := ""
		if n.Label != nil {
			label = " " + n.Label.Name
		}
		m := s.Mutants[0]
		fmt.Fprintf(buf, "if gamutCovering { gamutHit(%d); gamutInfect(%d) }; if gamutActive == %d { %s%s } else { %s%s }",
			s.Index, m.Index, m.Index, m.Replacement, label, n.Tok, label)

	case *ast.AssignStmt:
		// x op= v。v は 1 回だけ評価して引数で渡す。対象の式は本文に写すので、その中の箇所は個別ビルドへ回す。
		// 変異で値が変わるかは対象の値にもよる（x *= 1 と x /= 1 など）ので、到達したら感染したとみなす（実行して確かめる側に倒す）。
		if len(n.Lhs) != 1 || len(n.Rhs) != 1 {
			fallback()
			return
		}
		r.demoteWithin(sites, r.offset(n.Lhs[0].Pos()), r.offset(n.Lhs[0].End()))
		target := r.text(f, n.Lhs[0])
		v, ok := r.operand(f, sites, n.Rhs[0], "gamutV")
		if !ok {
			fallback()
			return
		}
		var body strings.Builder
		fmt.Fprintf(&body, "if gamutCovering { gamutHit(%d); ", s.Index)
		for _, m := range s.Mutants {
			fmt.Fprintf(&body, "gamutInfect(%d); ", m.Index)
		}
		body.WriteString("}; switch gamutActive { ")
		for _, m := range s.Mutants {
			fmt.Fprintf(&body, "case %d: %s %s %s; ", m.Index, target, m.Replacement, v.ref)
		}
		fmt.Fprintf(&body, "default: %s %s %s }", target, n.Tok, v.ref)
		if v.inline {
			fmt.Fprintf(buf, "func() { %s }()", body.String())
			return
		}
		fmt.Fprintf(buf, "func(%s %s) { %s }(", v.ref, v.typ, body.String())
		r.renderArgs(buf, f, sites, []operand{v})
		buf.WriteString(")")

	default:
		fallback()
	}
}

// renderLogical は、&& と || の箇所を書き換える。右辺は短絡評価で評価されないことがあるので、先に評価して引数に渡せない。
// 左辺だけを引数で渡し、右辺は関数リテラル gamutR の本文に 1 回だけ書いて、元の形と変異後の形のどちらでも必要なときだけ呼ぶ。
// 右辺の中の変異箇所は、その本文の中でそのまま埋め込む。
// 変異で値が変わるか（左辺と右辺が違うか）を知るには右辺を評価する必要があり、元のプログラムにない副作用を起こしうるので、
// 到達したら感染したとみなす。
func (r *renderer) renderLogical(buf *bytes.Buffer, f *load.File, sites []*Site, s *Site, n *ast.BinaryExpr, fallback func()) {
	resultType, ok := r.resultType(f, n)
	if !ok {
		fallback()
		return
	}
	x, ok := r.operand(f, sites, n.X, "gamutL")
	if !ok {
		fallback()
		return
	}
	fmt.Fprintf(buf, "func(%s) %s { gamutR := func() %s { return ", strings.Join(params(x), ""), resultType, resultType)
	r.renderRange(buf, f, sites, r.offset(n.Y.Pos()), r.offset(n.Y.End()))
	fmt.Fprintf(buf, " }; if gamutCovering { gamutHit(%d); ", s.Index)
	for _, m := range s.Mutants {
		fmt.Fprintf(buf, "gamutInfect(%d); ", m.Index)
	}
	buf.WriteString("}; switch gamutActive { ")
	for _, m := range s.Mutants {
		fmt.Fprintf(buf, "case %d: return %s %s gamutR(); ", m.Index, x.ref, m.Replacement)
	}
	fmt.Fprintf(buf, "}; return %s %s gamutR() }(", x.ref, n.Op)
	if !x.inline {
		r.renderArgs(buf, f, sites, []operand{x})
	}
	buf.WriteString(")")
}

// params は、引数で渡す被演算子の仮引数の宣言を返す。本文へ写す被演算子は引数にしない。
func params(o operand) []string {
	if o.inline {
		return nil
	}
	return []string{o.ref + " " + o.typ}
}

func (r *renderer) renderArgs(buf *bytes.Buffer, f *load.File, sites []*Site, args []operand) {
	for i, a := range args {
		if i > 0 {
			buf.WriteString(", ")
		}
		r.renderRange(buf, f, sites, r.offset(a.expr.Pos()), r.offset(a.expr.End()))
	}
}

func (r *renderer) resultType(f *load.File, e ast.Expr) (string, bool) {
	t := r.pkg.Info.Types[e].Type
	if t == nil {
		return "", false
	}
	return r.typeString(f, types.Default(t))
}

// typeString は、ファイル f の中で t を書く字句を返す。書けなければ false を返す。
func (r *renderer) typeString(f *load.File, t types.Type) (string, bool) {
	if t == nil {
		return "", false
	}
	if b, ok := t.(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		return "", false
	}
	names := importNames(r.pkg, f)
	ok := true
	s := types.TypeString(t, func(p *types.Package) string {
		if p == r.pkg.Types {
			return ""
		}
		name, found := names[p.Path()]
		if !found {
			ok = false
			return p.Path()
		}
		return name
	})
	return s, ok
}

// importNames は、ファイル f が import するパッケージのパスから、ファイルの中での名前への対応を返す。
func importNames(pkg *load.Package, f *load.File) map[string]string {
	byPath := map[string]string{}
	for _, imp := range pkg.Types.Imports() {
		byPath[imp.Path()] = imp.Name()
	}
	out := map[string]string{}
	for _, spec := range f.AST.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := byPath[path]
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				continue
			}
			name = spec.Name.Name
		}
		if name != "" {
			out[path] = name
		}
	}
	return out
}

func isInteger(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

// typeErrors は書き換え後のパッケージを型検査し、エラーの位置を含む最も内側の箇所を返す。
func typeErrors(pkg *load.Package, files map[string][]byte, helperPath string, helper []byte, ranges map[string][]outRange) ([]*Site, error) {
	fset := token.NewFileSet()
	var asts []*ast.File
	parse := func(path string, src []byte) error {
		f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse rewritten %s: %w", path, err)
		}
		asts = append(asts, f)
		return nil
	}
	for _, f := range append(slices.Clone(pkg.Files), pkg.Others...) {
		src := f.Src
		if rewritten, ok := files[f.Path]; ok {
			src = rewritten
		}
		if err := parse(f.Path, src); err != nil {
			return nil, err
		}
	}
	if err := parse(helperPath, helper); err != nil {
		return nil, err
	}

	_, errs := pkg.Check(fset, asts, nil)
	seen := map[*Site]bool{}
	var bad []*Site
	for _, e := range errs {
		pos := e.Fset.Position(e.Pos)
		var best *outRange
		for i := range ranges[pos.Filename] {
			rg := &ranges[pos.Filename][i]
			if pos.Offset >= rg.start && pos.Offset < rg.end && (best == nil || rg.end-rg.start < best.end-best.start) {
				best = rg
			}
		}
		if best == nil {
			return nil, fmt.Errorf("rewritten package does not type-check outside mutated code: %s", e.Error())
		}
		if !seen[best.site] {
			seen[best.site] = true
			bad = append(bad, best.site)
		}
	}
	return bad, nil
}

func renderHelper(pkgName string, sites, mutants, funcs int) []byte {
	return fmt.Appendf(nil, `// Code generated by gamut. DO NOT EDIT.

package %s

import (
	"os"
	"strconv"
	"sync"
	"sync/atomic"
)

var gamutActive = gamutEnvInt("GAMUT_MUTANT")

var gamutCovering = os.Getenv("GAMUT_COVER") != ""

var (
	gamutHits     [%d]atomic.Bool
	gamutInfected [%d]atomic.Bool
	gamutEntered  [%d]atomic.Bool
	gamutMu       sync.Mutex
	gamutOut      *os.File
)

func gamutEnvInt(name string) int {
	v, _ := strconv.Atoi(os.Getenv(name))
	return v
}

// gamutHit は到達を記録する。GAMUT_MUTANT が %d なら、選択の変数が読まれたことを示すために panic する。
func gamutHit(i int) {
	if gamutActive == %d {
		panic("gamut: forced fail")
	}
	gamutRecord(&gamutHits[i], 'h', i)
}

func gamutInfect(i int) { gamutRecord(&gamutInfected[i], 'i', i) }

// GamutSetActive は、有効にする変異体を i に切り替える。1 つのテストプロセスで変異体を順に判定するときに、テストの合間に呼ばれる。
func GamutSetActive(i int) { gamutActive = i }

// GamutCoverStop は、被覆の記録を止めて記録のファイルを閉じる。被覆を測ったテストプロセスを、そのまま変異体の判定に使い回すときに呼ばれる。
func GamutCoverStop() {
	gamutMu.Lock()
	defer gamutMu.Unlock()
	gamutCovering = false
	if gamutOut != nil {
		_ = gamutOut.Close()
		gamutOut = nil
	}
}

// GamutTestStart は、トップレベルのテスト name が始まったことを記録の境目として書き、以後の到達と感染と入った関数を記録し直す。
// 1 つのプロセスで複数のテストの被覆を記録するときに、テストの関数の先頭から呼ばれる。外部テストからも呼べるよう公開する。
func GamutTestStart(name string) {
	if !gamutCovering {
		return
	}
	gamutMu.Lock()
	defer gamutMu.Unlock()
	for i := range gamutHits {
		gamutHits[i].Store(false)
	}
	for i := range gamutInfected {
		gamutInfected[i].Store(false)
	}
	for i := range gamutEntered {
		gamutEntered[i].Store(false)
	}
	if gamutOpen() {
		_, _ = gamutOut.Write([]byte("t " + name + "\n"))
	}
}

// gamutOpen は、記録のファイルを開く。gamutMu を持って呼ぶ。
func gamutOpen() bool {
	if !gamutCovering {
		return false
	}
	if gamutOut == nil {
		f, err := os.OpenFile(os.Getenv("GAMUT_COVER"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			return false
		}
		gamutOut = f
	}
	return true
}

// gamutEnter は、関数に入ったことを記録する。
func gamutEnter(i int) { gamutRecord(&gamutEntered[i], 'e', i) }

// gamutMark は、到達だけを記録して v をそのまま返す。
func gamutMark[T any](i int, v T) T {
	if gamutCovering {
		gamutHit(i)
	}
	return v
}

// gamutRecord は、到達と感染を初回だけファイルへ追記する。
// テストバイナリは os.Exit で終わるので、終了時ではなく記録のたびに書き出す。
func gamutRecord(flag *atomic.Bool, kind byte, i int) {
	if flag.Load() {
		return
	}
	gamutMu.Lock()
	defer gamutMu.Unlock()
	if flag.Swap(true) || !gamutOpen() {
		return
	}
	line := append([]byte{kind, ' '}, strconv.Itoa(i)...)
	_, _ = gamutOut.Write(append(line, '\n'))
}
`, pkgName, sites+1, mutants+1, funcs+1, ForcedFail, ForcedFail)
}
