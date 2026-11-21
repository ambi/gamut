// Package mutant enumerates mutation sites and holds mutant identifiers and viability checks.
package mutant

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/constant"
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

// Type is the name of a mutator.
type Type string

// Mutators. The names follow Gremlins and gomutants.
const (
	ArithmeticBase       Type = "ARITHMETIC_BASE"
	ConditionalsBoundary Type = "CONDITIONALS_BOUNDARY"
	ConditionalsNegation Type = "CONDITIONALS_NEGATION"
	IncrementDecrement   Type = "INCREMENT_DECREMENT"
	InvertNegatives      Type = "INVERT_NEGATIVES"
	InvertBitwise        Type = "INVERT_BITWISE"
	// InvertAssignments and InvertBitwiseAssignments replace the operator of a compound assignment.
	InvertAssignments        Type = "INVERT_ASSIGNMENTS"
	InvertBitwiseAssignments Type = "INVERT_BITWISE_ASSIGNMENTS"
	// InvertLoopCtrl swaps break and continue.
	InvertLoopCtrl Type = "INVERT_LOOP_CTRL"
	// InvertLogical swaps && and ||.
	InvertLogical Type = "INVERT_LOGICAL"
	// RemoveLogicalNot removes the logical negation !.
	RemoveLogicalNot Type = "REMOVE_LOGICAL_NOT"
	// BooleanLiteral swaps the universe-scope true and false.
	BooleanLiteral Type = "BOOLEAN_LITERAL"
	// ConditionsConstant makes the conditions of if and for, and the operands of && and ||, constant.
	ConditionsConstant Type = "CONDITIONS_CONSTANT"
	// StatementRemove keeps a call statement that returns no value from running.
	StatementRemove Type = "STATEMENT_REMOVE"
	// ReturnZero returns the zero values of the result types at the start of the body of a function with results.
	ReturnZero Type = "RETURN_ZERO"
	// RemoveSelfAssignments turns a compound assignment into a plain one (x += y into x = y).
	RemoveSelfAssignments Type = "REMOVE_SELF_ASSIGNMENTS"
	// IntegerIncrement and IntegerDecrement add 1 to and subtract 1 from an integer literal.
	IntegerIncrement Type = "INTEGER_INCREMENT"
	IntegerDecrement Type = "INTEGER_DECREMENT"
	// FloatIncrement and FloatDecrement add 1 to and subtract 1 from a floating-point literal.
	FloatIncrement Type = "FLOAT_INCREMENT"
	FloatDecrement Type = "FLOAT_DECREMENT"
	// BranchIf, BranchElse, and BranchCase empty the body of an if, an else, or a case of a switch.
	BranchIf   Type = "BRANCH_IF"
	BranchElse Type = "BRANCH_ELSE"
	BranchCase Type = "BRANCH_CASE"
	// ErrorfWrap turns %w in the format of fmt.Errorf into %v, breaking the error chain.
	ErrorfWrap Type = "ERRORF_WRAP"
	// ReturnErrorNil turns a return's result of type error into nil.
	ReturnErrorNil Type = "RETURN_ERROR_NIL"
)

// ErrorSite is the mutation site of RETURN_ERROR_NIL. It wraps the result expression X of a return, making it a site apart from X's own.
type ErrorSite struct {
	X ast.Expr
}

// Pos is the start of the result expression.
func (e *ErrorSite) Pos() token.Pos { return e.X.Pos() }

// End is the end of the result expression.
func (e *ErrorSite) End() token.Pos { return e.X.End() }

// emptiedID is the replacement part of a BRANCH_* identifier. The replacement text contains the body, so a fixed string is used.
const emptiedID = "<emptied>"

// BranchSite is the mutation site of BRANCH_*. Its range is the statement list of the branch body (inside { and } of an if or else block, after : of a case).
// The mutation wraps the body in if false { ... }, keeping references so that variables used only in the body stay used.
type BranchSite struct {
	// Branch is an *ast.BlockStmt or an *ast.CaseClause.
	Branch   ast.Node
	From, To token.Pos
}

// Pos is the start of the body.
func (b *BranchSite) Pos() token.Pos { return b.From }

// End is the end of the body.
func (b *BranchSite) End() token.Pos { return b.To }

// zeroID is the replacement part of a RETURN_ZERO identifier. The replacement text contains the result types, so a fixed string is used.
const zeroID = "<zero>"

// BodySite is the mutation site of RETURN_ZERO. Its range is just the one character { of the function body, making it a site apart from those inside the body.
type BodySite struct {
	Func *ast.FuncDecl
}

// Pos is the position of the body's {.
func (b *BodySite) Pos() token.Pos { return b.Func.Body.Lbrace }

// End is right after the body's {.
func (b *BodySite) End() token.Pos { return b.Func.Body.Lbrace + 1 }

// removedID is the replacement part of a STATEMENT_REMOVE identifier. The replacement text contains the statement,
// so the identifier uses a fixed string, keeping it unchanged when the arguments are rewritten.
const removedID = "<removed>"

// ConditionSite is the mutation site of CONDITIONS_CONSTANT. It wraps the condition expression X, making it a site apart from X's own.
// Its range equals X's; schemata rewrites the wrapping site first and the sites inside X within it.
type ConditionSite struct {
	X ast.Expr
}

// Pos is the start of the condition expression.
func (c *ConditionSite) Pos() token.Pos { return c.X.Pos() }

// End is the end of the condition expression.
func (c *ConditionSite) End() token.Pos { return c.X.End() }

// AllTypes are the mutators enabled by default. RETURN_ZERO and STATEMENT_REMOVE joined the default because, measured on real packages,
// their survivors pointed at gaps in the tests (functions whose results are not checked, side effects not checked) and they added 10 to 20 percent to the time.
var AllTypes = []Type{ArithmeticBase, ConditionalsBoundary, ConditionalsNegation, IncrementDecrement, InvertNegatives, ReturnZero, StatementRemove}

// OptIn are the mutators off by default, enabled only when chosen with -only or in the config.
// Whether one joins the default set is decided after measuring the number of survivors and their breakdown on real packages.
var OptIn = []Type{InvertBitwise, InvertAssignments, InvertBitwiseAssignments, InvertLoopCtrl, InvertLogical, RemoveLogicalNot, BooleanLiteral, ConditionsConstant,
	RemoveSelfAssignments, IntegerIncrement, IntegerDecrement, FloatIncrement, FloatDecrement, BranchIf, BranchElse, BranchCase,
	ErrorfWrap, ReturnErrorNil}

// Known are all the mutators gamut knows. Names in -only, the config, disable comments, and acceptances are checked against it.
var Known = slices.Concat(AllTypes, OptIn)

// Mutant is a mutant that replaces one range of the source (from Offset, the length of OriginalText) with ReplacementText.
// For an operator mutation, the range is one operator token, and Original and Replacement are those tokens.
// For a mutation a token cannot express (such as removing !), Original and Replacement are token.ILLEGAL and only the strings are set.
type Mutant struct {
	// Index is the number that selects the mutant in the schemata, starting at 1.
	Index int
	// ID is the identifier, independent of lines and columns.
	ID   string
	Type Type
	File *load.File
	// Offset is the byte offset in the file where the replaced range starts.
	Offset      int
	Original    token.Token
	Replacement token.Token
	// OriginalText and ReplacementText are the text before and after replacement. For an operator mutation they equal the tokens' text.
	OriginalText    string
	ReplacementText string
	Line            int
	Column          int
	// Site is the syntax tree node of the mutation site (*ast.BinaryExpr, *ast.UnaryExpr, *ast.IncDecStmt, or the *ast.AssignStmt of a compound assignment).
	Site ast.Node
	// Scope is the name of the declaration enclosing the mutation site.
	Scope string
	// Viable reports whether the mutated package passes type-checking.
	Viable bool
	// SiteKey is a key of the mutation site independent of lines and columns. Mutants at the same site share it.
	// Carrying coverage records over across runs uses it instead of the site numbers, which change with every rewrite.
	SiteKey string

	// path is the path through the syntax tree within the enclosing declaration.
	path []int
	// idTo is the replacement part put in the identifier. It is usually ReplacementText, and a fixed string for mutations containing statements or types.
	idTo string
}

// Key returns the hash part of the identifier. It serves as a short key telling mutants apart within a package.
func (m *Mutant) Key() string {
	return m.ID[strings.LastIndex(m.ID, "::")+2:]
}

// IDAt returns the identifier the mutant would have if its file were at the module-relative path rel.
// It finds the identifier from before a file rename, to carry over the previous verdict.
func (m *Mutant) IDAt(rel string) string {
	return identity(rel, m.Scope, m.path, m.Type, m.idTo)
}

// setID builds the identifier with the replacement part to.
func (m *Mutant) setID(rel string, to string) {
	m.idTo = to
	m.ID = identity(rel, m.Scope, m.path, m.Type, to)
}

// rule replaces the token from with to. When to is token.ILLEGAL, it removes from.
type rule struct {
	typ  Type
	from token.Token
	to   token.Token
}

// replacement returns the rule's replacement text.
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

// branchRules are the rules for break and continue statements. goto and fallthrough are not mutated.
var branchRules = []rule{
	{InvertLoopCtrl, token.BREAK, token.CONTINUE},
	{InvertLoopCtrl, token.CONTINUE, token.BREAK},
}

// assignRules are the rules for compound assignment statements. Plain assignments (=, :=) are not mutated.
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
	{RemoveSelfAssignments, token.ADD_ASSIGN, token.ASSIGN},
	{RemoveSelfAssignments, token.SUB_ASSIGN, token.ASSIGN},
	{RemoveSelfAssignments, token.MUL_ASSIGN, token.ASSIGN},
	{RemoveSelfAssignments, token.QUO_ASSIGN, token.ASSIGN},
	{RemoveSelfAssignments, token.REM_ASSIGN, token.ASSIGN},
	{RemoveSelfAssignments, token.AND_ASSIGN, token.ASSIGN},
	{RemoveSelfAssignments, token.OR_ASSIGN, token.ASSIGN},
	{RemoveSelfAssignments, token.XOR_ASSIGN, token.ASSIGN},
	{RemoveSelfAssignments, token.AND_NOT_ASSIGN, token.ASSIGN},
	{RemoveSelfAssignments, token.SHL_ASSIGN, token.ASSIGN},
	{RemoveSelfAssignments, token.SHR_ASSIGN, token.ASSIGN},
}

var incDecRules = []rule{
	{IncrementDecrement, token.INC, token.DEC},
	{IncrementDecrement, token.DEC, token.INC},
}

// Enumerate enumerates, with the default arid rules, the mutants of the mutators in enabled across the package's target files.
func Enumerate(pkg *load.Package, enabled []Type) []*Mutant {
	out, _, _ := EnumerateWith(pkg, enabled, &Arid{})
	return out
}

// EnumerateWith enumerates mutants with the arid rules arid. The second result is the number of mutants not created because they are in arid code.
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

// Without removes from ms the mutants for which drop returns true and renumbers the rest from 1.
// The schemata use the numbers as indexes into an array in the helper file, so no gaps are left.
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

// enumerateFile enumerates the mutants of a file. Mutants in arid code are counted instead of created.
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
	// parentOf returns the parent of the node being visited, or nil for the file.
	parentOf := func() ast.Node {
		if len(stack) < 2 {
			return nil
		}
		return stack[len(stack)-2].node
	}
	// enclosingSignature returns the signature of the innermost function (declaration or function literal) enclosing the node being visited.
	// The node itself is not included (the function literal in return func() error {...} is a result of the enclosing function).
	enclosingSignature := func() *types.Signature {
		for i := len(stack) - 2; i >= 0; i-- {
			switch fn := stack[i].node.(type) {
			case *ast.FuncLit:
				sig, _ := pkg.Info.Types[fn].Type.(*types.Signature)
				return sig
			case *ast.FuncDecl:
				if obj, ok := pkg.Info.Defs[fn.Name].(*types.Func); ok {
					return obj.Signature()
				}
				return nil
			}
		}
		return nil
	}
	// allow is the arid depth emit ignores. A call building an error message (fmt.Errorf) is arid,
	// but mutants that change that very call (ERRORF_WRAP, RETURN_ERROR_NIL) are not stopped by its aridity.
	allow := 0
	emit := func(m *Mutant) {
		if aridDepth > allow {
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
			// Top-level declarations of the file are the units of identifiers.
			scope = declName(n)
		}
		if isArid {
			aridDepth++
		}
		var rules []rule
		var opPos token.Pos
		var op token.Token
		// add creates a mutant at the mutation site n. The replacement part put in the identifier is idTo, or toText when empty.
		add := func(typ Type, at token.Pos, from, to token.Token, fromText, toText, idTo string) {
			// The identifier's path is relative to the declaration, so declarations added before it do not change it.
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
		// For a condition expression, list the mutants making the condition constant before the mutants inside it.
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
						// The wrapping site has the same range as the expression's own, so a mark added to the path makes a different key.
						SiteKey: siteKey(file.Rel, scope, append(slices.Clone(rel), -1)), path: slices.Clone(rel),
					}
					m.setID(file.Rel, c)
					emit(m)
				}
			}
		}
		// For a return's result of type error, list the mutant turning it into nil before the mutants inside it.
		if expr, ok := n.(ast.Expr); ok && slices.Contains(enabled, ReturnErrorNil) && returnedError(pkg.Info, parentOf(), expr, enclosingSignature()) {
			rel := path[2:]
			pos := pkg.Fset.Position(expr.Pos())
			text := string(file.Src[pos.Offset:pkg.Fset.Position(expr.End()).Offset])
			m := &Mutant{
				Type: ReturnErrorNil, File: file, Offset: pos.Offset,
				// Keep the expression in a branch that never runs, so that variables and imports used only in it stay used.
				Original: token.ILLEGAL, Replacement: token.ILLEGAL, OriginalText: text, ReplacementText: "func() error { if false { return " + text + " }; return nil }()",
				Line: pos.Line, Column: pos.Column, Site: &ErrorSite{X: expr}, Scope: scope,
				// The wrapping site has the same range as the expression's own, so a mark added to the path makes a different key.
				SiteKey: siteKey(file.Rel, scope, append(slices.Clone(rel), -4)), path: slices.Clone(rel),
			}
			m.setID(file.Rel, "nil")
			if isArid {
				allow = 1
			}
			emit(m)
			allow = 0
		}
		// For a branch body, list the mutant emptying the body before the mutants inside it.
		if typ, from, to, ok := branchBody(parentOf(), n); ok && slices.Contains(enabled, typ) && !arid.allArid(branchStmts(n)) {
			start, end := pkg.Fset.Position(from).Offset, pkg.Fset.Position(to).Offset
			text := string(file.Src[start:end])
			pos := pkg.Fset.Position(from)
			rel := path[2:]
			m := &Mutant{
				Type: typ, File: file, Offset: start,
				Original: token.ILLEGAL, Replacement: token.ILLEGAL, OriginalText: text, ReplacementText: "if false {" + text + "}",
				Line: pos.Line, Column: pos.Column, Site: &BranchSite{Branch: n, From: from, To: to}, Scope: scope,
				SiteKey: siteKey(file.Rel, scope, rel), path: slices.Clone(rel),
			}
			m.setID(file.Rel, emptiedID)
			emit(m)
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
			// Insert a statement returning the zero values of the result types right after the { of the body of a function with results. The types copy the signature's text, so they can always be written in that file.
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
			// Turn a call statement that returns no value into if false { ... }. The statement is kept rather than deleted, so that variables and imports used only in the arguments stay used.
			// Logging call statements are excluded by the arid rules and counted.
			if call, ok := x.X.(*ast.CallExpr); ok && slices.Contains(enabled, StatementRemove) && returnsNothing(pkg.Info, call) {
				text := string(file.Src[pkg.Fset.Position(x.Pos()).Offset:pkg.Fset.Position(x.End()).Offset])
				add(StatementRemove, x.Pos(), token.ILLEGAL, token.ILLEGAL, text, "if false { "+text+" }", removedID)
			}
			return true
		case *ast.BasicLit:
			for _, lm := range literalMutants(pkg.Info, x, parentOf(), enabled) {
				add(lm.typ, x.Pos(), token.ILLEGAL, token.ILLEGAL, x.Value, lm.text, "")
			}
			if slices.Contains(enabled, ErrorfWrap) && errorfFormat(pkg.Info, parentOf(), x) {
				if to, ok := unwrapVerbs(x.Value); ok {
					if stack[len(stack)-2].arid {
						allow = 1
					}
					add(ErrorfWrap, x.Pos(), token.ILLEGAL, token.ILLEGAL, x.Value, to, "")
					allow = 0
				}
			}
			return true
		case *ast.Ident:
			// Mutate only the universe-scope true and false. Local variables of the same name are not mutated.
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

// conditionConstants returns, when expr, a child of parent, is a condition, the constants CONDITIONS_CONSTANT replaces it with.
//   - The condition of an if: true and false.
//   - The condition of a for: only false. true makes an infinite loop, which only waits for the timeout and is slow.
//   - An operand of &&: only true (dropping that condition). An operand of ||: only false.
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

// zeroValues returns the zero values (*new(T)) of the function's result types, separated by commas. It returns empty when there are no results, no body, or for init.
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

// branchBody returns, when n is an if or else block or a case clause of a switch, its mutator and the range of the body's statement list.
// Clauses with an empty body and clauses ending in fallthrough (emptied, they could not hold the fallthrough) are not returned. Clauses of select are not targets.
func branchBody(parent, n ast.Node) (Type, token.Pos, token.Pos, bool) {
	switch x := n.(type) {
	case *ast.BlockStmt:
		ifs, ok := parent.(*ast.IfStmt)
		if !ok || len(x.List) == 0 {
			return "", 0, 0, false
		}
		switch {
		case ifs.Body == x:
			return BranchIf, x.Lbrace + 1, x.Rbrace, true
		case ifs.Else == x:
			return BranchElse, x.Lbrace + 1, x.Rbrace, true
		}
	case *ast.CaseClause:
		if len(x.Body) == 0 {
			return "", 0, 0, false
		}
		if br, ok := x.Body[len(x.Body)-1].(*ast.BranchStmt); ok && br.Tok == token.FALLTHROUGH {
			return "", 0, 0, false
		}
		return BranchCase, x.Colon + 1, x.End(), true
	}
	return "", 0, 0, false
}

// branchStmts returns the statement list of a branch body.
func branchStmts(n ast.Node) []ast.Stmt {
	switch x := n.(type) {
	case *ast.BlockStmt:
		return x.List
	case *ast.CaseClause:
		return x.Body
	}
	return nil
}

// returnedError reports whether expr is a result of a return, the result type at that position of the enclosing function sig is error, and expr is not nil.
// A concrete result type that satisfies error (such as *MyErr) is not turned into nil: such a nil may not compare equal to a nil error.
func returnedError(info *types.Info, parent ast.Node, expr ast.Expr, sig *types.Signature) bool {
	ret, ok := parent.(*ast.ReturnStmt)
	if !ok || sig == nil || sig.Results().Len() != len(ret.Results) || info.Types[expr].IsNil() {
		return false
	}
	i := slices.Index(ret.Results, expr)
	return i >= 0 && types.Identical(sig.Results().At(i).Type(), types.Universe.Lookup("error").Type())
}

// errorfFormat reports whether lit is the format argument of fmt.Errorf.
func errorfFormat(info *types.Info, parent ast.Node, lit *ast.BasicLit) bool {
	call, ok := parent.(*ast.CallExpr)
	if !ok || lit.Kind != token.STRING || len(call.Args) == 0 || call.Args[0] != lit {
		return false
	}
	fn, ok := callee(info, call).(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == "fmt" && fn.Name() == "Errorf"
}

// unwrapVerbs returns the format literal s with every %w turned into %v, or false when there is no %w.
// %% is not a verb and is skipped; flags, width, precision, and argument indexes (%[1]w) are kept.
func unwrapVerbs(s string) (string, bool) {
	b := []byte(s)
	changed := false
	for i := 0; i < len(b); i++ {
		if b[i] != '%' {
			continue
		}
		j := i + 1
		for j < len(b) && strings.IndexByte("+-# 0123456789.*[]", b[j]) >= 0 {
			j++
		}
		if j < len(b) && b[j] == 'w' {
			b[j] = 'v'
			changed = true
		}
		i = j
	}
	return string(b), changed
}

// literalMutant is the mutator and replacement text of a literal mutant.
type literalMutant struct {
	typ  Type
	text string
}

// literalMutants returns the mutants adding 1 to and subtracting 1 from the numeric literal lit. parent is lit's parent node.
// The replacement is written in decimal and parenthesized when negative (so a-0 does not become a--1). A float always has a decimal point or an exponent, so it does not become an integer constant.
// The capacity of make and the size hint of a map are not mutated, as changing them does not change behavior.
func literalMutants(info *types.Info, lit *ast.BasicLit, parent ast.Node, enabled []Type) []literalMutant {
	if sizeHint(info, lit, parent) {
		return nil
	}
	var out []literalMutant
	switch lit.Kind {
	case token.INT:
		v := constant.MakeFromLiteral(lit.Value, token.INT, 0)
		if v.Kind() != constant.Int {
			return nil
		}
		for _, d := range []struct {
			typ Type
			op  token.Token
		}{{IntegerIncrement, token.ADD}, {IntegerDecrement, token.SUB}} {
			if slices.Contains(enabled, d.typ) {
				out = append(out, literalMutant{d.typ, signed(constant.BinaryOp(v, d.op, constant.MakeInt64(1)).String())})
			}
		}
	case token.FLOAT:
		f, _ := constant.Float64Val(constant.MakeFromLiteral(lit.Value, token.FLOAT, 0))
		for _, d := range []struct {
			typ Type
			to  float64
		}{{FloatIncrement, f + 1}, {FloatDecrement, f - 1}} {
			if d.to == f || !slices.Contains(enabled, d.typ) {
				// Values too large to show a difference of 1 are not mutated.
				continue
			}
			s := strconv.FormatFloat(d.to, 'g', -1, 64)
			if !strings.ContainsAny(s, ".eE") {
				s += ".0"
			}
			out = append(out, literalMutant{d.typ, signed(s)})
		}
	}
	return out
}

// signed parenthesizes the text of a negative number.
func signed(s string) string {
	if strings.HasPrefix(s, "-") {
		return "(" + s + ")"
	}
	return s
}

// sizeHint reports whether lit is the capacity of make (the third argument) or the size hint of make for a map (the second argument).
func sizeHint(info *types.Info, lit *ast.BasicLit, parent ast.Node) bool {
	call, ok := parent.(*ast.CallExpr)
	if !ok || len(call.Args) < 2 {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	if b, ok := info.Uses[id].(*types.Builtin); !ok || b.Name() != "make" {
		return false
	}
	if len(call.Args) == 3 && call.Args[2] == lit {
		return true
	}
	_, isMap := info.Types[call.Args[0]].Type.Underlying().(*types.Map)
	return isMap && call.Args[1] == lit
}

// returnsNothing reports whether call calls a function that returns no value. Conversions and builtin calls are not included (panic is).
func returnsNothing(info *types.Info, call *ast.CallExpr) bool {
	tv, ok := info.Types[call]
	if !ok || tv.IsType() {
		return false
	}
	tuple, ok := tv.Type.(*types.Tuple)
	return ok && tuple.Len() == 0
}

// isUniverseBool reports whether id refers to the universe-scope constant true or false.
func isUniverseBool(info *types.Info, id *ast.Ident) bool {
	if id.Name != "true" && id.Name != "false" {
		return false
	}
	return info.Uses[id] == types.Universe.Lookup(id.Name)
}

// identity builds an identifier. to is the replacement text; for an operator mutation it is the token's text, so the identifier is the same as before the representation was widened.
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

// DeclName returns the name of the top-level declaration n in the same form as the declaration part of mutant identifiers (F for a function, T.M for a method).
func DeclName(n ast.Node) string { return declName(n) }

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

// Apply returns the file contents with the mutant's range replaced.
func (m *Mutant) Apply(src []byte) []byte {
	out := make([]byte, 0, len(src)+len(m.ReplacementText))
	out = append(out, src[:m.Offset]...)
	out = append(out, m.ReplacementText...)
	out = append(out, src[m.Offset+len(m.OriginalText):]...)
	return out
}

// CheckViability type-checks the mutated package for each mutant and sets Viable.
func CheckViability(pkg *load.Package, mutants []*Mutant) {
	CheckViabilityWith(pkg, mutants, nil)
}

// CheckViabilityWith uses known's result for mutants whose result it knows (second result true),
// type-checks the rest in parallel up to the number of CPUs, and sets Viable.
// The imported objects the checks read were resolved by the package's first type-check, so reading them in parallel is safe.
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
