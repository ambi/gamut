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

// ForcedFail は、GAMUT_MUTANT に渡すと、到達した変異箇所で panic させる値である。
// 変異の選択がテストに届くことを、判定の前に確かめるために使う。
const ForcedFail = -1

// helperNames は補助ファイルが宣言する名前である。対象パッケージと衝突してはならない。
var helperNames = []string{"gamutActive", "gamutCovering", "gamutHits", "gamutInfected", "gamutMu", "gamutOut", "gamutEnvInt", "gamutHit", "gamutInfect", "gamutRecord", "gamutMark"}

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
	if _, ok := s.Node.(*ast.IncDecStmt); ok {
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
		r := &renderer{pkg: pkg}
		files := map[string][]byte{}
		for _, f := range pkg.Files {
			var inFile []*Site
			for _, s := range sites {
				if s.File == f && s.Level != Untouched {
					inFile = append(inFile, s)
				}
			}
			if len(inFile) == 0 {
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

		helper := renderHelper(pkg.Name, indexed, len(mutants))
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
		return &Result{Files: files, HelperPath: helperPath, Helper: helper, Sites: sites, SiteOf: siteOf}, nil
	}
	return nil, fmt.Errorf("schemata rewrite of %s did not converge", pkg.ImportPath)
}

type outRange struct {
	site       *Site
	start, end int
}

type renderer struct {
	pkg *load.Package
	// demotions は、描画中に書き換えられないと分かった箇所と、次に試す Level である。
	demotions map[*Site]Level
	// ranges は、ファイルごとに各箇所が書き換え後の内容のどこを占めるかである。
	ranges map[string][]outRange
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
		return b.end - a.end
	})
	if r.ranges == nil {
		r.ranges = map[string][]outRange{}
	}
	var buf bytes.Buffer
	r.renderRange(&buf, f, sites, 0, len(f.Src))
	return buf.Bytes()
}

// renderRange は元のソースの [lo, hi) を buf へ書き、その中の変異箇所を書き換える。
func (r *renderer) renderRange(buf *bytes.Buffer, f *load.File, sites []*Site, lo, hi int) {
	pos := lo
	for _, s := range sites {
		if s.start < pos || s.end > hi {
			continue
		}
		buf.Write(f.Src[pos:s.start])
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
	buf.Write(f.Src[pos:hi])
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
			fmt.Fprintf(&body, "if (%s%s) != (%s%s) { gamutInfect(%d) }; ", m.Replacement, x.ref, n.Op, x.ref, m.Index)
		}
		body.WriteString("}; switch gamutActive { ")
		for _, m := range s.Mutants {
			fmt.Fprintf(&body, "case %d: return %s%s; ", m.Index, m.Replacement, x.ref)
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

	default:
		fallback()
	}
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

func renderHelper(pkgName string, sites, mutants int) []byte {
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
	if flag.Swap(true) {
		return
	}
	if gamutOut == nil {
		f, err := os.OpenFile(os.Getenv("GAMUT_COVER"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			return
		}
		gamutOut = f
	}
	line := append([]byte{kind, ' '}, strconv.Itoa(i)...)
	_, _ = gamutOut.Write(append(line, '\n'))
}
`, pkgName, sites+1, mutants+1, ForcedFail, ForcedFail)
}
