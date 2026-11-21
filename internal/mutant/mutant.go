// Package mutant は、変異箇所の列挙、変異体の識別子、構築可能性の判定を持つ。
package mutant

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/ambi/gamut/internal/load"
)

// Type は変異演算子の名前である。
type Type string

// 変異演算子。名前は Gremlins と gomutants に合わせる。
const (
	ArithmeticBase       Type = "ARITHMETIC_BASE"
	ConditionalsBoundary Type = "CONDITIONALS_BOUNDARY"
	ConditionalsNegation Type = "CONDITIONALS_NEGATION"
	IncrementDecrement   Type = "INCREMENT_DECREMENT"
	InvertNegatives      Type = "INVERT_NEGATIVES"
	InvertBitwise        Type = "INVERT_BITWISE"
	// InvertAssignments と InvertBitwiseAssignments は、複合代入の演算子を置き換える。
	InvertAssignments        Type = "INVERT_ASSIGNMENTS"
	InvertBitwiseAssignments Type = "INVERT_BITWISE_ASSIGNMENTS"
	// InvertLoopCtrl は、break と continue を入れ替える。
	InvertLoopCtrl Type = "INVERT_LOOP_CTRL"
	// InvertLogical は、&& と || を入れ替える。
	InvertLogical Type = "INVERT_LOGICAL"
	// RemoveLogicalNot は、論理否定の ! を取り除く。
	RemoveLogicalNot Type = "REMOVE_LOGICAL_NOT"
	// BooleanLiteral は、宇宙スコープの true と false を入れ替える。
	BooleanLiteral Type = "BOOLEAN_LITERAL"
	// ConditionsConstant は、if と for の条件と、&& と || の被演算子を定数にする。
	ConditionsConstant Type = "CONDITIONS_CONSTANT"
	// StatementRemove は、値を返さない呼び出しの文を実行しないようにする。
	StatementRemove Type = "STATEMENT_REMOVE"
	// ReturnZero は、戻り値を持つ関数の本体の先頭で、戻り値の型のゼロ値を返すようにする。
	ReturnZero Type = "RETURN_ZERO"
)

// zeroID は、RETURN_ZERO の識別子の置き換え後の部分である。置き換え後の文字列は戻り値の型を含むので、固定の文字列を使う。
const zeroID = "<zero>"

// BodySite は、RETURN_ZERO の変異箇所である。関数の本体の { の 1 字だけを範囲にし、本体の中の箇所とは別の箇所にする。
type BodySite struct {
	Func *ast.FuncDecl
}

// Pos は本体の { の位置である。
func (b *BodySite) Pos() token.Pos { return b.Func.Body.Lbrace }

// End は本体の { の直後である。
func (b *BodySite) End() token.Pos { return b.Func.Body.Lbrace + 1 }

// removedID は、STATEMENT_REMOVE の識別子の置き換え後の部分である。置き換え後の文字列は文の中身を含むので、
// 引数を書き換えても識別子が変わらないよう、識別子には固定の文字列を使う。
const removedID = "<removed>"

// ConditionSite は、CONDITIONS_CONSTANT の変異箇所である。条件の式 X を包み、X そのものの変異箇所とは別の箇所にする。
// 範囲は X と同じで、schemata は包む側を先に書き換え、X の中の箇所をその中で書き換える。
type ConditionSite struct {
	X ast.Expr
}

// Pos は条件の式の先頭である。
func (c *ConditionSite) Pos() token.Pos { return c.X.Pos() }

// End は条件の式の終わりである。
func (c *ConditionSite) End() token.Pos { return c.X.End() }

// AllTypes は既定で有効な変異演算子である。RETURN_ZERO と STATEMENT_REMOVE は、実在のパッケージでの測定で、
// 生き残りがテストの穴（結果を確かめていない関数、確かめていない副作用）を示し、時間の増加が 1〜2 割だったので既定に入れた。
var AllTypes = []Type{ArithmeticBase, ConditionalsBoundary, ConditionalsNegation, IncrementDecrement, InvertNegatives, ReturnZero, StatementRemove}

// OptIn は、既定では無効で、-only や設定で選んだときだけ有効になる変異演算子である。
// 実在のパッケージで生き残りの数と仕分けの内訳を測ってから、既定の集合に入れるかを決める。
var OptIn = []Type{InvertBitwise, InvertAssignments, InvertBitwiseAssignments, InvertLoopCtrl, InvertLogical, RemoveLogicalNot, BooleanLiteral, ConditionsConstant}

// Known は、gamut が知っているすべての変異演算子である。-only、設定、無効化コメント、受理の名前はこれで確かめる。
var Known = slices.Concat(AllTypes, OptIn)

// Mutant は、ソースの 1 つの区間（Offset から OriginalText の長さ）を ReplacementText に置き換える変異体である。
// 演算子の変異では、区間は演算子の字句 1 つで、Original と Replacement はその字句である。
// 字句で表せない変異（! の削除など）では、Original と Replacement は token.ILLEGAL で、文字列だけを持つ。
type Mutant struct {
	// Index は schemata の中で変異体を選ぶ番号で、1 から始まる。
	Index int
	// ID は行と列に依存しない識別子である。
	ID   string
	Type Type
	File *load.File
	// Offset はファイルの中での置き換える区間の先頭のバイト位置である。
	Offset      int
	Original    token.Token
	Replacement token.Token
	// OriginalText と ReplacementText は、置き換える前と後の文字列である。演算子の変異では字句の文字列と同じである。
	OriginalText    string
	ReplacementText string
	Line            int
	Column          int
	// Site は変異箇所の構文木のノード（*ast.BinaryExpr、*ast.UnaryExpr、*ast.IncDecStmt、複合代入の *ast.AssignStmt）である。
	Site ast.Node
	// Scope は変異箇所を囲む宣言の名前である。
	Scope string
	// Viable は、変異後のパッケージが型検査を通るかである。
	Viable bool
	// SiteKey は、変異箇所の行と列に依存しない鍵である。同じ箇所の変異体は同じ鍵を持つ。
	// 被覆の記録を実行をまたいで引き継ぐときに、書き換えごとに変わる箇所の番号の代わりに使う。
	SiteKey string

	// path は、囲む宣言の中での構文木の経路である。
	path []int
	// idTo は、識別子に入れる置き換え後の部分である。ふつうは ReplacementText で、文や型を含む変異では固定の文字列である。
	idTo string
}

// Key は、識別子のハッシュの部分を返す。パッケージの中で変異体を区別する短い鍵として使う。
func (m *Mutant) Key() string {
	return m.ID[strings.LastIndex(m.ID, "::")+2:]
}

// IDAt は、変異体のファイルがモジュール相対のパス rel にあったときの識別子を返す。
// ファイルの名前の変更の前の識別子を求め、前の判定を引き継ぐために使う。
func (m *Mutant) IDAt(rel string) string {
	return identity(rel, m.Scope, m.path, m.Type, m.idTo)
}

// setID は、置き換え後の部分 to で識別子を作る。
func (m *Mutant) setID(rel string, to string) {
	m.idTo = to
	m.ID = identity(rel, m.Scope, m.path, m.Type, to)
}

// rule は、字句 from を to に置き換える規則である。to が token.ILLEGAL なら、from を取り除く。
type rule struct {
	typ  Type
	from token.Token
	to   token.Token
}

// replacement は、規則の置き換え後の文字列を返す。
func (r rule) replacement() string {
	if r.to == token.ILLEGAL {
		return ""
	}
	return r.to.String()
}

var binaryRules = []rule{
	{ArithmeticBase, token.ADD, token.SUB},
	{ArithmeticBase, token.SUB, token.ADD},
	{ArithmeticBase, token.MUL, token.QUO},
	{ArithmeticBase, token.QUO, token.MUL},
	{ArithmeticBase, token.REM, token.MUL},
	{ConditionalsBoundary, token.LSS, token.LEQ},
	{ConditionalsBoundary, token.LEQ, token.LSS},
	{ConditionalsBoundary, token.GTR, token.GEQ},
	{ConditionalsBoundary, token.GEQ, token.GTR},
	{ConditionalsNegation, token.EQL, token.NEQ},
	{ConditionalsNegation, token.NEQ, token.EQL},
	{ConditionalsNegation, token.LSS, token.GEQ},
	{ConditionalsNegation, token.LEQ, token.GTR},
	{ConditionalsNegation, token.GTR, token.LEQ},
	{ConditionalsNegation, token.GEQ, token.LSS},
	{InvertBitwise, token.AND, token.OR},
	{InvertBitwise, token.OR, token.AND},
	{InvertBitwise, token.XOR, token.AND},
	{InvertBitwise, token.AND_NOT, token.AND},
	{InvertBitwise, token.SHL, token.SHR},
	{InvertBitwise, token.SHR, token.SHL},
	{InvertLogical, token.LAND, token.LOR},
	{InvertLogical, token.LOR, token.LAND},
}

var unaryRules = []rule{
	{InvertNegatives, token.SUB, token.ADD},
	{RemoveLogicalNot, token.NOT, token.ILLEGAL},
}

// branchRules は break と continue の文の規則である。goto と fallthrough は変異しない。
var branchRules = []rule{
	{InvertLoopCtrl, token.BREAK, token.CONTINUE},
	{InvertLoopCtrl, token.CONTINUE, token.BREAK},
}

// assignRules は複合代入の文の規則である。単純な代入（=、:=）は変異しない。
var assignRules = []rule{
	{InvertAssignments, token.ADD_ASSIGN, token.SUB_ASSIGN},
	{InvertAssignments, token.SUB_ASSIGN, token.ADD_ASSIGN},
	{InvertAssignments, token.MUL_ASSIGN, token.QUO_ASSIGN},
	{InvertAssignments, token.QUO_ASSIGN, token.MUL_ASSIGN},
	{InvertAssignments, token.REM_ASSIGN, token.MUL_ASSIGN},
	{InvertBitwiseAssignments, token.AND_ASSIGN, token.OR_ASSIGN},
	{InvertBitwiseAssignments, token.OR_ASSIGN, token.AND_ASSIGN},
	{InvertBitwiseAssignments, token.XOR_ASSIGN, token.AND_ASSIGN},
	{InvertBitwiseAssignments, token.AND_NOT_ASSIGN, token.AND_ASSIGN},
	{InvertBitwiseAssignments, token.SHL_ASSIGN, token.SHR_ASSIGN},
	{InvertBitwiseAssignments, token.SHR_ASSIGN, token.SHL_ASSIGN},
}

var incDecRules = []rule{
	{IncrementDecrement, token.INC, token.DEC},
	{IncrementDecrement, token.DEC, token.INC},
}

// Enumerate は、パッケージの対象ファイルから、enabled に含まれる変異演算子の変異体を、既定の arid の規則で列挙する。
func Enumerate(pkg *load.Package, enabled []Type) []*Mutant {
	out, _, _ := EnumerateWith(pkg, enabled, &Arid{})
	return out
}

// EnumerateWith は、arid の規則 arid で変異体を列挙する。2 つ目の戻り値は、arid のコードの中なので作らなかった変異体の数である。
func EnumerateWith(pkg *load.Package, enabled []Type, arid *Arid) ([]*Mutant, int, error) {
	if err := arid.Validate(); err != nil {
		return nil, 0, err
	}
	checker := &aridChecker{info: pkg.Info, rule: arid}
	var out []*Mutant
	skipped := 0
	for _, file := range pkg.Files {
		ms, n := enumerateFile(pkg, file, enabled, checker)
		out = append(out, ms...)
		skipped += n
	}
	number(out)
	return out, skipped, nil
}

// Without は、ms から drop が真を返す変異体を除き、残りの番号を 1 から振り直す。
// schemata は番号を補助ファイルの配列の添字に使うので、番号に隙間を残さない。
func Without(ms []*Mutant, drop func(*Mutant) bool) []*Mutant {
	out := slices.DeleteFunc(ms, drop)
	number(out)
	return out
}

func number(ms []*Mutant) {
	for i, m := range ms {
		m.Index = i + 1
	}
}

// enumerateFile は、ファイルの変異体を列挙する。arid のコードの中の変異体は作らずに数える。
func enumerateFile(pkg *load.Package, file *load.File, enabled []Type, arid *aridChecker) ([]*Mutant, int) {
	var out []*Mutant
	skipped := 0
	type frame struct {
		node     ast.Node
		children int
		arid     bool
	}
	var stack []frame
	var path []int
	scope := ""
	aridDepth := 0
	emit := func(m *Mutant) {
		if aridDepth > 0 {
			skipped++
			return
		}
		out = append(out, m)
	}

	ast.Inspect(file.AST, func(n ast.Node) bool {
		if n == nil {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			path = path[:len(path)-1]
			if top.arid {
				aridDepth--
			}
			return true
		}
		index := 0
		if len(stack) > 0 {
			index = stack[len(stack)-1].children
			stack[len(stack)-1].children++
		}
		isArid := arid.node(n)
		stack = append(stack, frame{node: n, arid: isArid})
		path = append(path, index)
		if len(stack) == 2 {
			// ファイル直下の宣言が識別子の単位になる。
			scope = declName(n)
		}
		if isArid {
			aridDepth++
		}
		var rules []rule
		var opPos token.Pos
		var op token.Token
		// add は、変異箇所 n の変異体を作る。識別子に入れる置き換え後の部分は idTo で、空なら toText である。
		add := func(typ Type, at token.Pos, from, to token.Token, fromText, toText, idTo string) {
			// 識別子の経路は宣言の中での相対位置にし、前に宣言が増えても変わらないようにする。
			rel := path[2:]
			pos := pkg.Fset.Position(at)
			m := &Mutant{
				Type: typ, File: file, Offset: pos.Offset,
				Original: from, Replacement: to, OriginalText: fromText, ReplacementText: toText,
				Line: pos.Line, Column: pos.Column, Site: n, Scope: scope,
				SiteKey: siteKey(file.Rel, scope, rel), path: slices.Clone(rel),
			}
			if idTo == "" {
				idTo = toText
			}
			m.setID(file.Rel, idTo)
			emit(m)
		}
		// 条件の式なら、その中の変異体より先に、条件を定数にする変異体を並べる。
		if expr, ok := n.(ast.Expr); ok && len(path) > 2 && slices.Contains(enabled, ConditionsConstant) && pkg.Info.Types[expr].Value == nil {
			if consts := conditionConstants(stack[len(stack)-2].node, expr); len(consts) > 0 {
				rel := path[2:]
				site := &ConditionSite{X: expr}
				pos := pkg.Fset.Position(expr.Pos())
				text := string(file.Src[pos.Offset:pkg.Fset.Position(expr.End()).Offset])
				for _, c := range consts {
					m := &Mutant{
						Type: ConditionsConstant, File: file, Offset: pos.Offset,
						Original: token.ILLEGAL, Replacement: token.ILLEGAL, OriginalText: text, ReplacementText: c,
						Line: pos.Line, Column: pos.Column, Site: site, Scope: scope,
						// 包む箇所は式そのものの箇所と範囲が同じなので、経路に印を足して別の鍵にする。
						SiteKey: siteKey(file.Rel, scope, append(slices.Clone(rel), -1)), path: slices.Clone(rel),
					}
					m.setID(file.Rel, c)
					emit(m)
				}
			}
		}
		switch x := n.(type) {
		case *ast.BinaryExpr:
			rules, opPos, op = binaryRules, x.OpPos, x.Op
		case *ast.UnaryExpr:
			rules, opPos, op = unaryRules, x.OpPos, x.Op
		case *ast.IncDecStmt:
			rules, opPos, op = incDecRules, x.TokPos, x.Tok
		case *ast.AssignStmt:
			rules, opPos, op = assignRules, x.TokPos, x.Tok
		case *ast.BranchStmt:
			rules, opPos, op = branchRules, x.TokPos, x.Tok
		case *ast.FuncDecl:
			// 戻り値を持つ関数の本体の { の直後に、戻り値の型のゼロ値を返す文を差し込む。型はシグネチャの字句を写すので、そのファイルで必ず書ける。
			if zeros := zeroValues(file, pkg.Fset, x); zeros != "" && slices.Contains(enabled, ReturnZero) {
				site := &BodySite{Func: x}
				pos := pkg.Fset.Position(site.Pos())
				m := &Mutant{
					Type: ReturnZero, File: file, Offset: pos.Offset,
					Original: token.ILLEGAL, Replacement: token.ILLEGAL, OriginalText: "{", ReplacementText: "{ return " + zeros + ";",
					Line: pos.Line, Column: pos.Column, Site: site, Scope: scope,
					SiteKey: siteKey(file.Rel, scope, []int{-2}),
				}
				m.setID(file.Rel, zeroID)
				emit(m)
			}
			return true
		case *ast.ExprStmt:
			// 値を返さない呼び出しの文を if false { ... } にする。引数でだけ使う変数や import が使われなくならないよう、文は消さずに残す。
			// ログ出力の呼び出しの文は arid の規則で除かれ、数えられる。
			if call, ok := x.X.(*ast.CallExpr); ok && slices.Contains(enabled, StatementRemove) && returnsNothing(pkg.Info, call) {
				text := string(file.Src[pkg.Fset.Position(x.Pos()).Offset:pkg.Fset.Position(x.End()).Offset])
				add(StatementRemove, x.Pos(), token.ILLEGAL, token.ILLEGAL, text, "if false { "+text+" }", removedID)
			}
			return true
		case *ast.Ident:
			// 宇宙スコープの true と false だけを変異する。同じ名前の局所変数は変異しない。
			if slices.Contains(enabled, BooleanLiteral) && isUniverseBool(pkg.Info, x) {
				add(BooleanLiteral, x.Pos(), token.ILLEGAL, token.ILLEGAL, x.Name, map[string]string{"true": "false", "false": "true"}[x.Name], "")
			}
			return true
		default:
			return true
		}
		for _, r := range rules {
			if r.from == op && slices.Contains(enabled, r.typ) {
				add(r.typ, opPos, r.from, r.to, r.from.String(), r.replacement(), "")
			}
		}
		return true
	})
	return out, skipped
}

// conditionConstants は、parent の子 expr が条件なら、CONDITIONS_CONSTANT で置き換える定数を返す。
//   - if の条件：true と false。
//   - for の条件：false だけ。true は無限ループになり、時間切れを待つだけで遅い。
//   - && の被演算子：true だけ（その条件を外す）。|| の被演算子：false だけ。
func conditionConstants(parent ast.Node, expr ast.Expr) []string {
	switch p := parent.(type) {
	case *ast.IfStmt:
		if p.Cond == expr {
			return []string{"true", "false"}
		}
	case *ast.ForStmt:
		if p.Cond == expr {
			return []string{"false"}
		}
	case *ast.BinaryExpr:
		if p.X != expr && p.Y != expr {
			return nil
		}
		switch p.Op {
		case token.LAND:
			return []string{"true"}
		case token.LOR:
			return []string{"false"}
		}
	}
	return nil
}

// zeroValues は、関数の戻り値の型のゼロ値（*new(T)）をカンマで並べて返す。戻り値がない、本体がない、init なら空を返す。
func zeroValues(file *load.File, fset *token.FileSet, fd *ast.FuncDecl) string {
	if fd.Body == nil || fd.Type.Results == nil || fd.Name.Name == "init" && fd.Recv == nil {
		return ""
	}
	var zeros []string
	for _, field := range fd.Type.Results.List {
		typ := string(file.Src[fset.Position(field.Type.Pos()).Offset:fset.Position(field.Type.End()).Offset])
		for range max(1, len(field.Names)) {
			zeros = append(zeros, "*new("+typ+")")
		}
	}
	return strings.Join(zeros, ", ")
}

// returnsNothing は、call が値を返さない関数の呼び出しかを返す。型の変換や組み込み関数の呼び出しは含めない（panic は含める）。
func returnsNothing(info *types.Info, call *ast.CallExpr) bool {
	tv, ok := info.Types[call]
	if !ok || tv.IsType() {
		return false
	}
	tuple, ok := tv.Type.(*types.Tuple)
	return ok && tuple.Len() == 0
}

// isUniverseBool は、id が宇宙スコープの定数 true か false を指すかを返す。
func isUniverseBool(info *types.Info, id *ast.Ident) bool {
	if id.Name != "true" && id.Name != "false" {
		return false
	}
	return info.Uses[id] == types.Universe.Lookup(id.Name)
}

// identity は識別子を作る。to は置き換え後の文字列で、演算子の変異では字句の文字列なので、表現を広げる前と同じ識別子になる。
func identity(rel, scope string, path []int, typ Type, to string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{rel, scope, joinPath(path), string(typ), to}, "\x00")))
	return fmt.Sprintf("%s::%s::%s::%s", rel, typ, scope, hex.EncodeToString(sum[:5]))
}

func siteKey(rel, scope string, path []int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{rel, scope, joinPath(path)}, "\x00")))
	return hex.EncodeToString(sum[:5])
}

func joinPath(path []int) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ".")
}

func declName(n ast.Node) string {
	switch d := n.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) > 0 {
			return recvName(d.Recv.List[0].Type) + "." + d.Name.Name
		}
		return d.Name.Name
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.ValueSpec:
				if len(s.Names) > 0 {
					return s.Names[0].Name
				}
			case *ast.TypeSpec:
				return s.Name.Name
			}
		}
	}
	return "_"
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "_"
}

// Apply は、変異体の区間を置き換えたファイルの内容を返す。
func (m *Mutant) Apply(src []byte) []byte {
	out := make([]byte, 0, len(src)+len(m.ReplacementText))
	out = append(out, src[:m.Offset]...)
	out = append(out, m.ReplacementText...)
	out = append(out, src[m.Offset+len(m.OriginalText):]...)
	return out
}

// CheckViability は、変異体ごとに変異後のパッケージを型検査し、Viable を設定する。
func CheckViability(pkg *load.Package, mutants []*Mutant) {
	CheckViabilityWith(pkg, mutants, nil)
}

// CheckViabilityWith は、known が結果を知っている変異体（2 つ目の戻り値が真）はその結果を使い、
// 残りの変異体を CPU の数まで並列に型検査して、Viable を設定する。
// 検査が読む import 先のオブジェクトは、パッケージの最初の型検査で解決済みなので、並列に読んでよい。
func CheckViabilityWith(pkg *load.Package, mutants []*Mutant, known func(*Mutant) (viable, ok bool)) {
	var todo []*Mutant
	for _, m := range mutants {
		if known != nil {
			if v, ok := known(m); ok {
				m.Viable = v
				continue
			}
		}
		todo = append(todo, m)
	}
	ch := make(chan *Mutant)
	var wg sync.WaitGroup
	for range min(runtime.NumCPU(), max(len(todo), 1)) {
		wg.Go(func() {
			for m := range ch {
				m.Viable = typeChecks(pkg, m)
			}
		})
	}
	for _, m := range todo {
		ch <- m
	}
	close(ch)
	wg.Wait()
}

func typeChecks(pkg *load.Package, m *Mutant) bool {
	f, err := parser.ParseFile(pkg.Fset, m.File.Path, m.Apply(m.File.Src), parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	files := make([]*ast.File, 0, len(pkg.Files)+len(pkg.Others))
	for _, other := range pkg.AllASTs() {
		if other == m.File.AST {
			files = append(files, f)
		} else {
			files = append(files, other)
		}
	}
	_, errs := pkg.Check(pkg.Fset, files, nil)
	return len(errs) == 0
}
