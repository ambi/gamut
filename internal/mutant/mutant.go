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
)

// AllTypes は既定で有効な変異演算子である。
var AllTypes = []Type{ArithmeticBase, ConditionalsBoundary, ConditionalsNegation, IncrementDecrement, InvertNegatives}

// OptIn は、既定では無効で、-only や設定で選んだときだけ有効になる変異演算子である。
// 実在のパッケージで生き残りの数と仕分けの内訳を測ってから、既定の集合に入れるかを決める。
var OptIn = []Type{InvertBitwise, InvertAssignments, InvertBitwiseAssignments}

// Known は、gamut が知っているすべての変異演算子である。-only、設定、無効化コメント、受理の名前はこれで確かめる。
var Known = slices.Concat(AllTypes, OptIn)

// Mutant は、演算子の字句 1 つを置き換える変異体である。
type Mutant struct {
	// Index は schemata の中で変異体を選ぶ番号で、1 から始まる。
	Index int
	// ID は行と列に依存しない識別子である。
	ID   string
	Type Type
	File *load.File
	// Offset はファイルの中での演算子の字句のバイト位置である。
	Offset      int
	Original    token.Token
	Replacement token.Token
	Line        int
	Column      int
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
}

// Key は、識別子のハッシュの部分を返す。パッケージの中で変異体を区別する短い鍵として使う。
func (m *Mutant) Key() string {
	return m.ID[strings.LastIndex(m.ID, "::")+2:]
}

// IDAt は、変異体のファイルがモジュール相対のパス rel にあったときの識別子を返す。
// ファイルの名前の変更の前の識別子を求め、前の判定を引き継ぐために使う。
func (m *Mutant) IDAt(rel string) string {
	return identity(rel, m.Scope, m.path, m.Type, m.Replacement)
}

type rule struct {
	typ  Type
	from token.Token
	to   token.Token
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
}

var unaryRules = []rule{{InvertNegatives, token.SUB, token.ADD}}

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

// aridPackages は、呼び出しの引数を変異しないパッケージである。ログ出力はテストが観測しないことが多い。
var aridPackages = map[string]bool{"log": true, "log/slog": true}

// Enumerate は、パッケージの対象ファイルから、enabled に含まれる変異演算子の変異体を列挙する。
func Enumerate(pkg *load.Package, enabled []Type) []*Mutant {
	var out []*Mutant
	for _, file := range pkg.Files {
		out = append(out, enumerateFile(pkg, file, enabled)...)
	}
	number(out)
	return out
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

func enumerateFile(pkg *load.Package, file *load.File, enabled []Type) []*Mutant {
	var out []*Mutant
	type frame struct {
		node     ast.Node
		children int
	}
	var stack []frame
	var path []int
	scope := ""
	aridDepth := 0

	ast.Inspect(file.AST, func(n ast.Node) bool {
		if n == nil {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			path = path[:len(path)-1]
			if isArid(pkg.Info, top.node) {
				aridDepth--
			}
			return true
		}
		index := 0
		if len(stack) > 0 {
			index = stack[len(stack)-1].children
			stack[len(stack)-1].children++
		}
		stack = append(stack, frame{node: n})
		path = append(path, index)
		if len(stack) == 2 {
			// ファイル直下の宣言が識別子の単位になる。
			scope = declName(n)
		}
		if isArid(pkg.Info, n) {
			aridDepth++
		}
		if aridDepth > 0 {
			return true
		}
		var rules []rule
		var opPos token.Pos
		var op token.Token
		switch x := n.(type) {
		case *ast.BinaryExpr:
			rules, opPos, op = binaryRules, x.OpPos, x.Op
		case *ast.UnaryExpr:
			rules, opPos, op = unaryRules, x.OpPos, x.Op
		case *ast.IncDecStmt:
			rules, opPos, op = incDecRules, x.TokPos, x.Tok
		case *ast.AssignStmt:
			rules, opPos, op = assignRules, x.TokPos, x.Tok
		default:
			return true
		}
		// 識別子の経路は宣言の中での相対位置にし、前に宣言が増えても変わらないようにする。
		rel := path[2:]
		for _, r := range rules {
			if r.from != op || !slices.Contains(enabled, r.typ) {
				continue
			}
			pos := pkg.Fset.Position(opPos)
			m := &Mutant{
				Type:        r.typ,
				File:        file,
				Offset:      pos.Offset,
				Original:    r.from,
				Replacement: r.to,
				Line:        pos.Line,
				Column:      pos.Column,
				Site:        n,
				Scope:       scope,
				SiteKey:     siteKey(file.Rel, scope, rel),
				path:        slices.Clone(rel),
			}
			m.ID = identity(file.Rel, scope, rel, r.typ, r.to)
			out = append(out, m)
		}
		return true
	})
	return out
}

func identity(rel, scope string, path []int, typ Type, to token.Token) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{rel, scope, joinPath(path), string(typ), to.String()}, "\x00")))
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

func isArid(info *types.Info, n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	var ident *ast.Ident
	switch f := call.Fun.(type) {
	case *ast.SelectorExpr:
		ident = f.Sel
	case *ast.Ident:
		ident = f
	default:
		return false
	}
	obj := info.Uses[ident]
	return obj != nil && obj.Pkg() != nil && aridPackages[obj.Pkg().Path()]
}

// Apply は、変異体の字句を置き換えたファイルの内容を返す。
func (m *Mutant) Apply(src []byte) []byte {
	orig := m.Original.String()
	out := make([]byte, 0, len(src)+2)
	out = append(out, src[:m.Offset]...)
	out = append(out, m.Replacement.String()...)
	out = append(out, src[m.Offset+len(orig):]...)
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
