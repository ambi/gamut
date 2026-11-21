// Package schemata builds source with every viable mutant embedded.
// With every mutation disabled it behaves like the original source, and the environment variable GAMUT_MUTANT enables one mutant.
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

// HelperFileName is the name of the helper file holding selection and coverage recording.
const HelperFileName = "gamut_schemata.go"

// TestStartName is the name of the helper function, called at the start of test functions, that writes a boundary in the record.
const TestStartName = "GamutTestStart"

// ForcedFail is the value of GAMUT_MUTANT that makes every reached mutation site panic.
// It checks, before judging, that the mutant selection reaches the tests.
const ForcedFail = -1

// helperNames are the names the helper file declares. They must not collide with the target package.
var helperNames = []string{"gamutActive", "gamutCovering", "gamutHits", "gamutInfected", "gamutMu", "gamutOut", "gamutEnvInt", "gamutHit", "gamutInfect", "gamutRecord", "gamutMark", "gamutEntered", "gamutEnter", "gamutOpen", TestStartName, "GamutSetActive", "GamutCoverStop"}

// Level is how far a mutation site is rewritten.
type Level int

const (
	// Wrapped embeds the mutants and records reach and infection.
	Wrapped Level = iota
	// Marked cannot embed the mutants, but records reach by wrapping the site in gamutMark, which returns the value as is.
	// The mutants are built standalone and judged only with the tests that reached them.
	Marked
	// Untouched is not rewritten. The mutants are built standalone and judged with every test.
	Untouched
)

// Site is one mutation site (a node) and the mutants embedded there.
type Site struct {
	// Index is the number used in coverage records, starting at 1. It is 0 for Untouched.
	Index   int
	Level   Level
	Node    ast.Node
	File    *load.File
	Mutants []*mutant.Mutant

	start, end int
}

func (s *Site) next() Level {
	switch s.Node.(type) {
	case *ast.IncDecStmt, *ast.AssignStmt, *ast.BranchStmt, *ast.ExprStmt, *mutant.BodySite, *mutant.BranchSite:
		// A statement returns no value, so it cannot be wrapped in gamutMark.
		return Untouched
	case *ast.BasicLit:
		// An untyped constant passed to gamutMark takes its default type (such as int), and the outer expression's types no longer match.
		return Untouched
	}
	return s.Level + 1
}

// Result is the result of the rewrite.
type Result struct {
	// Files maps the absolute paths of the original files to their rewritten contents.
	Files map[string][]byte
	// HelperPath and Helper are the path and contents of the helper file added with the overlay.
	HelperPath string
	Helper     []byte
	// Sites are all the mutation sites with viable mutants.
	Sites []*Site
	// SiteOf maps each viable mutant to its mutation site.
	SiteOf map[*mutant.Mutant]*Site
	// Funcs are the function declarations whose entry coverage recording records. The number of Funcs[i] is i+1.
	Funcs []*ast.FuncDecl
}

// Count returns the number of mutants at the given Level.
func (r *Result) Count(level Level) int {
	n := 0
	for _, s := range r.Sites {
		if s.Level == level {
			n += len(s.Mutants)
		}
	}
	return n
}

// Build builds source with the viable mutants embedded.
// Sites where the rewritten package fails type-checking are moved toward standalone builds until it passes.
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

	// A site in a context that needs a constant stops being constant when turned into a call. That error appears outside the site (at the start of the constant expression), where the site cannot be identified,
	// so such sites go to standalone builds before rewriting.
	constant := constantContexts(pkg)
	for _, s := range sites {
		if slices.ContainsFunc(constant, func(n ast.Node) bool { return n.Pos() <= s.Node.Pos() && s.Node.End() <= n.End() }) {
			s.Level = Untouched
		}
	}

	funcs, entries := entryPoints(pkg)

	// Each iteration raises the Level of at least one site, so the loop ends within twice the number of sites.
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
		// If rendering found sites it cannot rewrite, raise their Level and render again.
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

// constantContexts returns the syntax that needs constant expressions (const declarations and array type lengths).
func constantContexts(pkg *load.Package) []ast.Node {
	var out []ast.Node
	for _, f := range pkg.Files {
		ast.Inspect(f.AST, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.GenDecl:
				if x.Tok == token.CONST {
					out = append(out, x)
					return false
				}
			case *ast.ArrayType:
				if x.Len != nil {
					out = append(out, x.Len)
				}
			}
			return true
		})
	}
	return out
}

// entry is the record of entering a function, inserted right after the { of its body.
type entry struct {
	offset int
	text   string
}

// entryPoints numbers the function declarations with bodies in the target files and returns the insertion points per file.
// Coverage recording uses them to know the functions a test entered, even without reaching a mutation site.
// //go:nosplit functions are left out, because an added call may exceed their stack limit.
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
	// entries are the function entry insertions per file. They are inserted while copying the text outside mutation sites.
	entries map[string][]entry
	// demotions are the sites rendering found it cannot rewrite, with the Level to try next.
	demotions map[*Site]Level
	// ranges are, per file, where each site lies in the rewritten contents.
	ranges map[string][]outRange
	// top reports whether the next renderRange is the top-level call for the file.
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
		// Sites wrapping conditions and branch bodies may have the same range as the wrapped expression or statement, so they come first. The wrapped site is rewritten inside them.
		ca, cb := wraps(a.Node), wraps(b.Node)
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

// wraps reports whether the site n rewrites the sites within its range inside its own rewrite.
func wraps(n ast.Node) bool {
	switch n.(type) {
	case *mutant.ConditionSite, *mutant.BranchSite, *mutant.ErrorSite:
		return true
	}
	return false
}

// copySrc copies [lo, hi) of the original source to buf. For the file's top-level text, it inserts the function entry records.
// The { of a function declaration's body never appears inside a mutation site's expression, so looking at the text copied at the top level is enough.
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

// renderRange writes [lo, hi) of the original source to buf, rewriting the mutation sites in it.
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

// demoteWithin leaves the sites within [lo, hi) unrewritten. It applies to sites inside text copied into a body as is.
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

// operand is an operand passed as an argument of a function literal, or copied directly into its body.
type operand struct {
	expr   ast.Expr
	inline bool
	// ref is the reference in the body (a parameter name or the copied text).
	ref string
	// typ is the text of the type when passed as an argument.
	typ string
}

func (r *renderer) operand(f *load.File, sites []*Site, e ast.Expr, param string) (operand, bool) {
	tv := r.pkg.Info.Types[e]
	if tv.Value != nil || tv.IsNil() || pure(e) {
		// Constants, nil, and references without side effects are copied into the body. No type needs writing, and evaluating them again gives the same result.
		r.demoteWithin(sites, r.offset(e.Pos()), r.offset(e.End()))
		return operand{expr: e, inline: true, ref: "(" + r.text(f, e) + ")"}, true
	}
	typ, ok := r.typeString(f, tv.Type)
	if !ok {
		return operand{}, false
	}
	return operand{expr: e, ref: param, typ: typ}, true
}

// pure reports whether e is a reference containing no function call and no mutation site.
// Such an expression gives the same value however often it is evaluated, and if it panics, the original panics at the same place.
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
				// Integer division by zero panics, so count the value as changed without comparing.
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
			// The replacement text of REMOVE_LOGICAL_NOT is empty, so only the operand is returned.
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
		// The target expression is copied into the body twice, so the sites inside it go to standalone builds.
		r.demoteWithin(sites, r.offset(n.X.Pos()), r.offset(n.X.End()))
		target := r.text(f, n.X)
		m := s.Mutants[0]
		fmt.Fprintf(buf, "func() { if gamutCovering { gamutHit(%d); gamutInfect(%d) }; if gamutActive == %d { %s%s } else { %s%s } }()",
			s.Index, m.Index, m.Index, target, m.Replacement, target, n.Tok)

	case *mutant.BodySite:
		// RETURN_ZERO: right after the body's {, return the zero values when the mutant is active. Every entry into the function reaches it, and the returned values change, so it counts as infected.
		// The function entry record is also inserted right after {, but this site rewrites {, so it is written here.
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
		// STATEMENT_REMOVE: skip the call statement when the mutant is active. Mutation sites in the arguments are rewritten inside the branch that runs it.
		// The statement becomes two statements, so where only a simple statement is allowed (such as a for's post statement), type-checking after the rewrite sends it to a standalone build.
		m := s.Mutants[0]
		fmt.Fprintf(buf, "if gamutCovering { gamutHit(%d); gamutInfect(%d) }; if gamutActive != %d { ", s.Index, m.Index, m.Index)
		r.renderRange(buf, f, without(sites, s), s.start, s.end)
		buf.WriteString(" }")

	case *mutant.BranchSite:
		// BRANCH_*: skip the branch body when the mutant is active. Entering the body reaches it, and skipping the body may change behavior, so it counts as infected.
		// Mutation sites in the body are rewritten inside the branch that runs it. Mutants whose wrapping changes whether the function terminates are already excluded by the viability checks.
		m := s.Mutants[0]
		fmt.Fprintf(buf, " if gamutCovering { gamutHit(%d); gamutInfect(%d) }; if gamutActive != %d {", s.Index, m.Index, m.Index)
		r.renderRange(buf, f, without(sites, s), s.start, s.end)
		buf.WriteString("}")

	case *mutant.ErrorSite:
		// RETURN_ERROR_NIL: when the mutant is active, return nil without evaluating the expression; otherwise receive the expression as an error and return it.
		// Receiving it as an error makes a typed nil pointer a non-nil error, as in the original return. It counts as infected when the original is not nil.
		// Mutation sites in the expression are rewritten as is inside the branch that evaluates it.
		m := s.Mutants[0]
		fmt.Fprintf(buf, "func() error { switch gamutActive { case %d: return nil }; var gamutC error = ", m.Index)
		r.renderRange(buf, f, without(sites, s), s.start, s.end)
		fmt.Fprintf(buf, "; if gamutCovering { gamutHit(%d); if gamutC != nil { gamutInfect(%d) } }; return gamutC }()", s.Index, m.Index)

	case *mutant.ConditionSite:
		// CONDITIONS_CONSTANT: when the mutant is active, return the constant without evaluating the condition; otherwise evaluate the condition.
		// Mutation sites in the condition are rewritten as is inside the branch that evaluates it.
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
		// BOOLEAN_LITERAL: turn true or false into a call returning the type in place. The value always changes, so reaching it counts as infection.
		// Contexts where a call cannot be written, such as inside a const declaration, go to standalone builds through type-checking after the rewrite.
		resultType, ok := r.resultType(f, n)
		if !ok {
			fallback()
			return
		}
		m := s.Mutants[0]
		fmt.Fprintf(buf, "func() %s { if gamutCovering { gamutHit(%d); gamutInfect(%d) }; if gamutActive == %d { return %s }; return %s }()",
			resultType, s.Index, m.Index, m.Index, m.ReplacementText, n.Name)

	case *ast.BasicLit:
		// INTEGER_*, FLOAT_*, ERRORF_WRAP: turn the literal into a call returning the type in place. The value always changes, so reaching it counts as infection.
		// Contexts that need a constant, such as const declarations and array lengths, go to standalone builds through type-checking after the rewrite.
		resultType, ok := r.resultType(f, n)
		if !ok {
			fallback()
			return
		}
		fmt.Fprintf(buf, "func() %s { if gamutCovering { gamutHit(%d); ", resultType, s.Index)
		for _, m := range s.Mutants {
			fmt.Fprintf(buf, "gamutInfect(%d); ", m.Index)
		}
		buf.WriteString("}; switch gamutActive { ")
		for _, m := range s.Mutants {
			fmt.Fprintf(buf, "case %d: return %s; ", m.Index, m.ReplacementText)
		}
		fmt.Fprintf(buf, "}; return %s }()", n.Value)

	case *ast.BranchStmt:
		// break and continue act on a loop in the same function, so they cannot be wrapped in a function literal. The statement becomes two branches.
		// If the branches change whether the loop terminates and cause a missing return, type-checking after the rewrite sends it to a standalone build.
		label := ""
		if n.Label != nil {
			label = " " + n.Label.Name
		}
		m := s.Mutants[0]
		fmt.Fprintf(buf, "if gamutCovering { gamutHit(%d); gamutInfect(%d) }; if gamutActive == %d { %s%s } else { %s%s }",
			s.Index, m.Index, m.Index, m.Replacement, label, n.Tok, label)

	case *ast.AssignStmt:
		// x op= v. v is evaluated once and passed as an argument. The target expression is copied into the body, so the sites inside it go to standalone builds.
		// Whether the mutation changes the value also depends on the target's value (as with x *= 1 and x /= 1), so reaching it counts as infection (erring on the side of running to check).
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

// renderLogical rewrites a site of && or ||. Short-circuit evaluation may leave the right side unevaluated, so it cannot be evaluated first and passed as an argument.
// Only the left side is passed as an argument; the right side is written once in the body of the function literal gamutR, called only when needed by the original or the mutated form.
// Mutation sites in the right side are embedded as is in that body.
// Knowing whether the mutation changes the value (whether the left and right sides differ) would require evaluating the right side, which may cause side effects the original program does not,
// so reaching it counts as infection.
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

// params returns the parameter declaration of an operand passed as an argument. Operands copied into the body are not parameters.
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

// typeString returns the text that writes t within the file f, or false when it cannot be written.
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

// importNames maps the paths of the packages the file f imports to their names within the file.
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

// typeErrors type-checks the rewritten package and returns the innermost sites containing the error positions.
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
	gamutRuntime "runtime"
	"strconv"
	gamutStrings "strings"
	"sync"
	"sync/atomic"
)

var gamutActive = gamutEnvInt("GAMUT_MUTANT")

var gamutCovering = os.Getenv("GAMUT_COVER") != ""

// When gamutMaxDepth is positive, coverage recording skips reach and infection deeper than this, counting the frames of gamutModule functions from the test function.
var (
	gamutMaxDepth = gamutEnvInt("GAMUT_MAX_DEPTH")
	gamutModule   = os.Getenv("GAMUT_MODULE")
)

var (
	gamutHits     [%[2]d]atomic.Bool
	gamutDeep     [%[2]d]atomic.Bool
	gamutInfected [%[3]d]atomic.Bool
	gamutEntered  [%[4]d]atomic.Bool
	gamutMu       sync.Mutex
	gamutOut      *os.File
)

func gamutEnvInt(name string) int {
	v, _ := strconv.Atoi(os.Getenv(name))
	return v
}

// gamutHit records reach. When GAMUT_MUTANT is %[5]d, it panics to show that the selection variable was read.
// Reach beyond the depth limit is recorded as "d" instead of as reach.
func gamutHit(i int) {
	if gamutActive == %[5]d {
		panic("gamut: forced fail")
	}
	if gamutHits[i].Load() {
		return
	}
	if gamutMaxDepth > 0 && gamutTooDeep() {
		gamutRecord(&gamutDeep[i], 'd', i)
		return
	}
	gamutRecord(&gamutHits[i], 'h', i)
}

func gamutInfect(i int) {
	if gamutInfected[i].Load() || gamutMaxDepth > 0 && gamutTooDeep() {
		return
	}
	gamutRecord(&gamutInfected[i], 'i', i)
}

// gamutTooDeep reports whether the calling mutation site is deeper than gamutMaxDepth, counting the frames of gamutModule functions
// from the test function (the function testing.tRunner calls). The function containing the site counts as 1, and neither the test function itself nor gamut's helper functions count.
// Consecutive frames of functions of the same declaration (function literals, functions schemata wraps, recursion) count as one.
// Calls not under a test function (package initialization, goroutines a test started) are not limited.
func gamutTooDeep() bool {
	var pcs [256]uintptr
	n := gamutRuntime.Callers(3, pcs[:])
	frames := gamutRuntime.CallersFrames(pcs[:n])
	depth, last := 0, ""
	for {
		f, more := frames.Next()
		if f.Function == "testing.tRunner" {
			return depth-1 > gamutMaxDepth
		}
		if decl := gamutDecl(f.Function); decl != "" && decl != last {
			depth++
			last = decl
		}
		if !more {
			// A stack too deep to record counts as beyond the limit.
			return n == len(pcs)
		}
	}
}

// GamutSetActive switches the active mutant to i. It is called between tests when one test process judges mutants one after another.
func GamutSetActive(i int) { gamutActive = i }

// GamutCoverStop stops coverage recording and closes the record file. It is called when the test process that measured coverage is reused to judge mutants.
func GamutCoverStop() {
	gamutMu.Lock()
	defer gamutMu.Unlock()
	gamutCovering = false
	if gamutOut != nil {
		_ = gamutOut.Close()
		gamutOut = nil
	}
}

// GamutTestStart writes the start of the top-level test name as a boundary in the record, and records reach, infection, and entered functions afresh from there.
// It is called at the start of test functions when one process records the coverage of several tests. It is exported so external tests can call it.
func GamutTestStart(name string) {
	if !gamutCovering {
		return
	}
	gamutMu.Lock()
	defer gamutMu.Unlock()
	for i := range gamutHits {
		gamutHits[i].Store(false)
		gamutDeep[i].Store(false)
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

// gamutDecl returns, when the function name fn (<import path>.<name>[type arguments]) is a function of gamutModule, the name of its enclosing declaration.
// A function literal (<decl>.funcN, <decl>.funcN.M) maps to its enclosing declaration. gamut's helper functions and functions outside the module return empty.
func gamutDecl(fn string) string {
	fn, _, _ = gamutStrings.Cut(fn, "[")
	if !gamutStrings.HasPrefix(fn, gamutModule+"/") && !gamutStrings.HasPrefix(fn, gamutModule+".") {
		return ""
	}
	slash := gamutStrings.LastIndex(fn, "/") + 1
	if gamutStrings.Contains(fn[slash:], ".gamut") {
		return ""
	}
	for i := slash; i < len(fn); i++ {
		if gamutStrings.HasPrefix(fn[i:], ".func") && i+5 < len(fn) && fn[i+5] >= '0' && fn[i+5] <= '9' {
			return fn[:i]
		}
	}
	return fn
}

// gamutOpen opens the record file. Call it holding gamutMu.
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

// gamutEnter records entering a function.
func gamutEnter(i int) { gamutRecord(&gamutEntered[i], 'e', i) }

// gamutMark records only reach and returns v as is.
func gamutMark[T any](i int, v T) T {
	if gamutCovering {
		gamutHit(i)
	}
	return v
}

// gamutRecord appends reach and infection to the file, only the first time.
// The test binary ends with os.Exit, so it writes at every record rather than at exit.
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
