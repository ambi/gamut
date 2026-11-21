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

// Arid holds the rules for code that tests rarely observe (arid code). No mutants are created inside arid code.
// The rules judge by the package and name of the callee's types.Object, never by matching text.
type Arid struct {
	// Off disables both the default rules and the configured ones.
	Off bool
	// Calls are globs of arid functions and methods, matched against "<package>.<Func>" for functions and "<package>.<Type>.<Method>" for methods.
	// * matches any string, including / and ..
	Calls []string
	// Packages are the import paths of packages whose calls are arid, including the packages below them.
	Packages []string
}

// aridPackages are the packages whose calls are arid by default. Tests rarely observe logging, metrics, and tracing output.
var aridPackages = []string{"log", "log/slog", "expvar", "go.opentelemetry.io/otel"}

// loggingPackages are the packages whose Debug*, Info*, Warn*, and Error* methods are treated as logger methods.
// Elsewhere, a receiver counts as a logger only when its type name contains Log.
var loggingPackages = []string{"log/slog", "go.uber.org/zap", "github.com/sirupsen/logrus", "github.com/rs/zerolog", "github.com/go-logr/logr", "k8s.io/klog"}

// aridChecker applies the rules within one package.
type aridChecker struct {
	info *types.Info
	rule *Arid
}

// Validate checks the syntax of the globs in Calls.
func (a *Arid) Validate() error {
	for _, g := range a.Calls {
		if _, err := path.Match(slashless(g), ""); err != nil {
			return fmt.Errorf("arid call pattern %q: %w", g, err)
		}
	}
	return nil
}

// slashless turns / into a character path.Match does not treat specially, so that * in a glob matches across /.
func slashless(s string) string { return strings.ReplaceAll(s, "/", "|") }

// node reports whether n is arid. No mutants are created inside an arid node.
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

// call reports whether the call only produces output (logging, metrics, tracing, writing to stderr, or calls added in the config).
// Such calls have neither their arguments mutated nor their statements removed.
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

// message reports whether the call is panic, errors.New, or fmt.Errorf. Arithmetic in the message is not mutated, but removing a panic statement is kept.
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

// stmt reports whether the statement consists only of arid call statements (such as an if that only logs). The whole statement, condition included, is then arid.
// It is the statement form of Google's rule arid(n) = simple(n) ? expert(n) : children.every(arid).
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

// allArid reports whether the statement list consists only of arid statements (such as a branch body that only logs). Such a body is not emptied.
func (c *aridChecker) allArid(list []ast.Stmt) bool {
	return !c.rule.Off && len(list) > 0 && !slices.ContainsFunc(list, func(s ast.Stmt) bool { return !c.stmt(s) })
}

// timeArithmetic reports whether an operand of the arithmetic is a call to time.Now() or time.Since(). Tests rarely observe elapsed-time calculations.
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

// stringMethod reports whether the declaration is a String, GoString, or Error method (no parameters, returning string). Bodies for display are rarely observed.
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

// callee returns the callee's Object, or nil for a function value or a conversion.
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

// typeName returns the name of the type (with any pointer removed), or empty when it has none.
func typeName(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj().Name()
	}
	return ""
}

// isStderr reports whether e is os.Stderr.
func isStderr(info *types.Info, e ast.Expr) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	v, ok := info.Uses[sel.Sel].(*types.Var)
	return ok && v.Pkg() != nil && v.Pkg().Path() == "os" && v.Name() == "Stderr"
}

// under reports whether the import path p is one of roots or below one.
func under(p string, roots []string) bool {
	return slices.ContainsFunc(roots, func(r string) bool { return p == r || strings.HasPrefix(p, r+"/") })
}
