package mutant

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"slices"
	"strings"
)

// Arid は、テストが観測しないことの多いコード（arid）の規則である。arid のコードの中では変異体を作らない。
// 規則は呼び出し先の types.Object のパッケージと名前で判定し、字句の一致には頼らない。
type Arid struct {
	// Off なら、既定の規則も設定の規則も使わない。
	Off bool
	// Calls は、arid とする関数とメソッドの glob である。関数は "<パッケージ>.<名前>"、メソッドは "<パッケージ>.<型>.<名前>" に当てる。
	// * は / と . を含む任意の文字列に当たる。
	Calls []string
	// Packages は、呼び出しを arid とするパッケージの import パスである。その下のパッケージも含む。
	Packages []string
}

// aridPackages は、既定で呼び出しを arid とするパッケージである。ログ、メトリクス、トレースの出力はテストが観測しないことが多い。
var aridPackages = []string{"log", "log/slog", "expvar", "go.opentelemetry.io/otel"}

// loggingPackages は、Debug*、Info*、Warn*、Error* のメソッドをロガーのメソッドとみなすパッケージである。
// これら以外では、受け手の型の名前が Log を含むときだけロガーとみなす。
var loggingPackages = []string{"log/slog", "go.uber.org/zap", "github.com/sirupsen/logrus", "github.com/rs/zerolog", "github.com/go-logr/logr", "k8s.io/klog"}

// aridChecker は、1 つのパッケージで規則を当てる。
type aridChecker struct {
	info *types.Info
	rule *Arid
}

// Validate は、Calls の glob の構文を確かめる。
func (a *Arid) Validate() error {
	for _, g := range a.Calls {
		if _, err := path.Match(slashless(g), ""); err != nil {
			return fmt.Errorf("arid call pattern %q: %w", g, err)
		}
	}
	return nil
}

// slashless は、glob の * が / をまたいで当たるよう、/ を path.Match で特別でない文字にする。
func slashless(s string) string { return strings.ReplaceAll(s, "/", "|") }

// node は、n が arid かを返す。arid のノードの中では変異体を作らない。
func (c *aridChecker) node(n ast.Node) bool {
	if c.rule.Off {
		return false
	}
	switch x := n.(type) {
	case *ast.CallExpr:
		return c.call(x) || c.message(x)
	case *ast.ExprStmt:
		call, ok := x.X.(*ast.CallExpr)
		return ok && c.call(call)
	case *ast.IfStmt:
		return c.stmt(x)
	case *ast.FuncDecl:
		return stringMethod(x)
	case *ast.BinaryExpr:
		return c.timeArithmetic(x)
	}
	return false
}

// call は、呼び出しが出力だけのもの（ログ、メトリクス、トレース、標準エラーへの書き出し、設定で足したもの）かを返す。
// そのような呼び出しは、引数を変異せず、文の削除もしない。
func (c *aridChecker) call(call *ast.CallExpr) bool {
	obj := callee(c.info, call)
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	pkg := obj.Pkg().Path()
	if under(pkg, aridPackages) || under(pkg, c.rule.Packages) {
		return true
	}
	if fn, ok := obj.(*types.Func); ok {
		if recv := fn.Signature().Recv(); recv != nil && logMethod(fn.Name()) && (under(pkg, loggingPackages) || strings.Contains(typeName(recv.Type()), "Log")) {
			return true
		}
		if pkg == "fmt" && strings.HasPrefix(fn.Name(), "Fprint") && len(call.Args) > 0 && isStderr(c.info, call.Args[0]) {
			return true
		}
		key := pkg + "." + fn.Name()
		if recv := fn.Signature().Recv(); recv != nil {
			key = pkg + "." + typeName(recv.Type()) + "." + fn.Name()
		}
		for _, g := range c.rule.Calls {
			if ok, _ := path.Match(slashless(g), slashless(key)); ok {
				return true
			}
		}
	}
	return false
}

// message は、呼び出しが panic、errors.New、fmt.Errorf かを返す。メッセージの算術は変異しないが、panic の文の削除は残す。
func (c *aridChecker) message(call *ast.CallExpr) bool {
	obj := callee(c.info, call)
	switch o := obj.(type) {
	case *types.Builtin:
		return o.Name() == "panic"
	case *types.Func:
		if o.Pkg() == nil {
			return false
		}
		p, name := o.Pkg().Path(), o.Name()
		return p == "errors" && name == "New" || p == "fmt" && name == "Errorf"
	}
	return false
}

// stmt は、文が arid の呼び出しの文だけからなるかを返す（ログを出すだけの if など）。条件を含めて全体を arid とする。
// Google の規則 arid(n) = simple(n) ? expert(n) : children.every(arid) の、文についての形である。
func (c *aridChecker) stmt(s ast.Stmt) bool {
	switch x := s.(type) {
	case *ast.ExprStmt:
		call, ok := x.X.(*ast.CallExpr)
		return ok && c.call(call)
	case *ast.BlockStmt:
		return len(x.List) > 0 && !slices.ContainsFunc(x.List, func(s ast.Stmt) bool { return !c.stmt(s) })
	case *ast.IfStmt:
		return x.Init == nil && c.stmt(x.Body) && (x.Else == nil || c.stmt(x.Else))
	}
	return false
}

// allArid は、文の並びが arid の文だけからなるかを返す（ログを出すだけの分岐の本体など）。そのような本体は空にしない。
func (c *aridChecker) allArid(list []ast.Stmt) bool {
	return !c.rule.Off && len(list) > 0 && !slices.ContainsFunc(list, func(s ast.Stmt) bool { return !c.stmt(s) })
}

// timeArithmetic は、算術の被演算子が time.Now() か time.Since() の呼び出しかを返す。経過時間の計算はテストが観測しにくい。
func (c *aridChecker) timeArithmetic(b *ast.BinaryExpr) bool {
	switch b.Op {
	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM:
	default:
		return false
	}
	for _, e := range []ast.Expr{b.X, b.Y} {
		call, ok := ast.Unparen(e).(*ast.CallExpr)
		if !ok {
			continue
		}
		if fn, ok := callee(c.info, call).(*types.Func); ok && fn.Pkg() != nil && fn.Pkg().Path() == "time" && (fn.Name() == "Now" || fn.Name() == "Since") {
			return true
		}
	}
	return false
}

// stringMethod は、宣言が String、GoString、Error のメソッド（引数なしで string を返す）かを返す。表示のための本体は観測されにくい。
func stringMethod(fd *ast.FuncDecl) bool {
	if fd.Recv == nil || fd.Type.Params.NumFields() != 0 || fd.Type.Results.NumFields() != 1 {
		return false
	}
	switch fd.Name.Name {
	case "String", "GoString", "Error":
	default:
		return false
	}
	id, ok := fd.Type.Results.List[0].Type.(*ast.Ident)
	return ok && id.Name == "string"
}

// callee は、呼び出し先の Object を返す。関数の値や型の変換なら nil を返す。
func callee(info *types.Info, call *ast.CallExpr) types.Object {
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		return info.Uses[f.Sel]
	case *ast.Ident:
		return info.Uses[f]
	}
	return nil
}

func logMethod(name string) bool {
	for _, p := range []string{"Debug", "Info", "Warn", "Error"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// typeName は、型（ポインタを外す）の名前を返す。名前がなければ空を返す。
func typeName(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj().Name()
	}
	return ""
}

// isStderr は、e が os.Stderr かを返す。
func isStderr(info *types.Info, e ast.Expr) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	v, ok := info.Uses[sel.Sel].(*types.Var)
	return ok && v.Pkg() != nil && v.Pkg().Path() == "os" && v.Name() == "Stderr"
}

// under は、import パス p が roots のどれかか、その下にあるかを返す。
func under(p string, roots []string) bool {
	return slices.ContainsFunc(roots, func(r string) bool { return p == r || strings.HasPrefix(p, r+"/") })
}
