package schemata_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/schemata"
)

func build(t *testing.T, src string) *schemata.Result {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.27.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, classic)
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func levels(res *schemata.Result) map[string]schemata.Level {
	out := map[string]schemata.Level{}
	for _, s := range res.Sites {
		for _, m := range s.Mutants {
			out[string(m.Type)+" "+m.Original.String()] = s.Level
		}
	}
	return out
}

func TestBuild_NestedSitesEvaluateOperandsOnce(t *testing.T) {
	res := build(t, "package m\n\nfunc F(a, b, c int) bool { return a+b < c }\n")
	for _, s := range res.Sites {
		if s.Level != schemata.Wrapped {
			t.Fatalf("site %v level = %d, want Wrapped", s.Mutants[0].Type, s.Level)
		}
	}
	var src string
	for _, b := range res.Files {
		src = string(b)
	}
	// The outer comparison takes the rewritten inner addition once as an argument and does not copy the inner one into its body.
	if strings.Count(src, "gamutHit(2)") != 1 {
		t.Fatalf("the inner site must be rendered once, as the outer argument:\n%s", src)
	}
}

func TestBuild_CompoundAssignmentsAreEmbedded(t *testing.T) {
	dir := t.TempDir()
	src := "package m\n\nfunc next() int { return 3 }\n\nfunc F(a int) int {\n\ta += next()\n\ta <<= 1\n\treturn a\n}\n"
	for name, s := range map[string]string{"go.mod": "module m\n\ngo 1.27.4\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, []mutant.Type{mutant.InvertAssignments, mutant.InvertBitwiseAssignments, mutant.RemoveSelfAssignments})
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	got := levels(res)
	for _, key := range []string{"INVERT_ASSIGNMENTS +=", "INVERT_BITWISE_ASSIGNMENTS <<=", "REMOVE_SELF_ASSIGNMENTS +=", "REMOVE_SELF_ASSIGNMENTS <<="} {
		if got[key] != schemata.Wrapped {
			t.Errorf("%s: level %d, want Wrapped (levels %v)", key, got[key], got)
		}
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	// The call on the right side is written once as an argument, and the body refers to it by the parameter's name.
	if strings.Count(out, "}(next())") != 1 || strings.Contains(out, "= next()") || !strings.Contains(out, "a -= gamutV") || !strings.Contains(out, "a = gamutV") {
		t.Errorf("the right-hand side must be evaluated once and passed in:\n%s", out)
	}
	// The rewrite does not change lines.
	if strings.Count(out, "\n") != strings.Count(src, "\n") {
		t.Errorf("the rewrite shifted lines:\n%s", out)
	}
}

func TestBuild_LoopControlBecomesTwoBranches(t *testing.T) {
	dir := t.TempDir()
	// G's for has no break, so it terminates. Turning continue into break makes it non-terminating, giving a missing return.
	src := "package m\n\nfunc F(xs []int) int {\n\tn := 0\nouter:\n\tfor _, x := range xs {\n\t\tif x < 0 {\n\t\t\tcontinue outer\n\t\t}\n\t\tn++\n\t}\n\treturn n\n}\n\nfunc G(x int) int {\n\tfor {\n\t\tif x > 0 {\n\t\t\tcontinue\n\t\t}\n\t\treturn x\n\t}\n}\n"
	for name, s := range map[string]string{"go.mod": "module m\n\ngo 1.27.4\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, []mutant.Type{mutant.InvertLoopCtrl})
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Sites {
		want := schemata.Wrapped
		if s.Mutants[0].Scope == "G" {
			want = schemata.Untouched
		}
		if s.Level != want {
			t.Errorf("%s: level %d, want %d", s.Mutants[0].Scope, s.Level, want)
		}
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	if !strings.Contains(out, "{ break outer } else { continue outer }") {
		t.Errorf("the labelled continue is not split into two branches:\n%s", out)
	}
}

func TestBuild_LogicalOperatorsKeepShortCircuit(t *testing.T) {
	dir := t.TempDir()
	// In the original form, the call on the right side is not evaluated when the left side is false. The comparison inside the right side is a mutation site too.
	src := "package m\n\nfunc expensive(n int) int { return n }\n\nfunc F(a bool, n int) bool { return a && expensive(n) > 0 }\n"
	for name, s := range map[string]string{"go.mod": "module m\n\ngo 1.27.4\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, []mutant.Type{mutant.InvertLogical, mutant.ConditionalsBoundary})
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	got := levels(res)
	for _, key := range []string{"INVERT_LOGICAL &&", "CONDITIONALS_BOUNDARY >"} {
		if got[key] != schemata.Wrapped {
			t.Errorf("%s: level %d, want Wrapped (levels %v)", key, got[key], got)
		}
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	// The right side appears once, in the body of gamutR, which only the right operands of && and || call.
	if strings.Count(out, "expensive(n)") != 1 || !strings.Contains(out, "return (a) || gamutR()") || !strings.Contains(out, "return (a) && gamutR()") {
		t.Errorf("the right operand must stay lazy and appear once:\n%s", out)
	}
}

func TestBuild_BooleanLiteralsTakeTheirContextType(t *testing.T) {
	dir := t.TempDir()
	// Flag is a named bool. The true in the const declaration cannot become a call, so it goes to a standalone build.
	src := "package m\n\ntype Flag bool\n\nconst On = true\n\nfunc F() Flag {\n\tvar f Flag = true\n\treturn f\n}\n\nfunc G() bool { return false }\n"
	for name, s := range map[string]string{"go.mod": "module m\n\ngo 1.27.4\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, []mutant.Type{mutant.BooleanLiteral})
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]schemata.Level{"On": schemata.Untouched, "F": schemata.Wrapped, "G": schemata.Wrapped}
	for _, s := range res.Sites {
		if w := want[s.Mutants[0].Scope]; s.Level != w {
			t.Errorf("%s: level %d, want %d", s.Mutants[0].Scope, s.Level, w)
		}
	}
	if len(res.Sites) != 3 {
		t.Errorf("%d sites, want 3", len(res.Sites))
	}
}

func TestBuild_NumericLiteralsTakeTheirContextType(t *testing.T) {
	dir := t.TempDir()
	// Literals in const declarations and array lengths must be constants, so they go to standalone builds.
	// Turning M's 10 into a call would report the not-constant error outside the literal (at the 64), so it goes to a standalone build before rewriting.
	src := "package m\n\nconst N = 3\n\nconst M = 64 << 10\n\nvar A [2]int\n\nfunc F(x float64) float64 { return x * 2 }\n\nfunc G(xs []int) int { return xs[1] }\n"
	for name, s := range map[string]string{"go.mod": "module m\n\ngo 1.27.4\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, []mutant.Type{mutant.IntegerIncrement, mutant.IntegerDecrement})
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]schemata.Level{"N": schemata.Untouched, "M": schemata.Untouched, "A": schemata.Untouched, "F": schemata.Wrapped, "G": schemata.Wrapped}
	for _, s := range res.Sites {
		if w := want[s.Mutants[0].Scope]; s.Level != w {
			t.Errorf("%s: level %d, want %d", s.Mutants[0].Scope, s.Level, w)
		}
		if len(s.Mutants) != 2 {
			t.Errorf("%s: %d mutants in a site, want the increment and the decrement", s.Mutants[0].Scope, len(s.Mutants))
		}
	}
	// Two sites for M's 64 and 10.
	if len(res.Sites) != 6 {
		t.Errorf("%d sites, want 6", len(res.Sites))
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	// The 2 is in a float64 context, so it becomes a call returning float64.
	if !strings.Contains(out, "func() float64 {") || !strings.Contains(out, "return 3; ") {
		t.Errorf("the literal does not take its context type:\n%s", out)
	}
}

func TestBuild_EmptiedBranchesWrapTheirInnerSites(t *testing.T) {
	dir := t.TempDir()
	src := "package m\n\nfunc g(n int) {}\n\nfunc F(a, b int) int {\n\tn := 0\n\tif a > 0 {\n\t\tn = a + b\n\t} else {\n\t\tg(a)\n\t}\n\tswitch b {\n\tcase 1:\n\t\tn = b * 2\n\t}\n\treturn n\n}\n"
	for name, s := range map[string]string{"go.mod": "module m\n\ngo 1.27.4\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, []mutant.Type{mutant.BranchIf, mutant.BranchElse, mutant.BranchCase, mutant.ArithmeticBase})
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	got := map[mutant.Type]int{}
	for _, s := range res.Sites {
		got[s.Mutants[0].Type]++
		if s.Level != schemata.Wrapped {
			t.Errorf("%s site: level %d, want Wrapped", s.Mutants[0].Type, s.Level)
		}
	}
	if got[mutant.BranchIf] != 1 || got[mutant.BranchElse] != 1 || got[mutant.BranchCase] != 1 || got[mutant.ArithmeticBase] != 2 {
		t.Errorf("sites by type %v, want one of each branch and two arithmetic", got)
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	// The body runs unless the mutant is active, and mutation sites in the body are rewritten inside it.
	if !strings.Contains(out, "if gamutActive != 1 {\n\t\tn = func(") {
		t.Errorf("the emptied body does not wrap its inner site:\n%s", out)
	}
	if strings.Count(out, "\n") != strings.Count(src, "\n") {
		t.Errorf("the rewrite shifted lines:\n%s", out)
	}
}

func TestBuild_ReturnedErrorsWrapTheirInnerSites(t *testing.T) {
	dir := t.TempDir()
	src := "package m\n\nimport (\n\t\"errors\"\n\t\"fmt\"\n)\n\nvar errNeg = errors.New(\"neg\")\n\nfunc F(x int) (int, error) {\n\tif x < 0 {\n\t\treturn 0, fmt.Errorf(\"neg %d: %w\", x, errNeg)\n\t}\n\treturn x, nil\n}\n"
	for name, s := range map[string]string{"go.mod": "module m\n\ngo 1.27.4\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, []mutant.Type{mutant.ReturnErrorNil, mutant.ErrorfWrap})
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sites) != 2 {
		t.Fatalf("%d sites, want 2", len(res.Sites))
	}
	for _, s := range res.Sites {
		if s.Level != schemata.Wrapped {
			t.Errorf("%s site: level %d, want Wrapped", s.Mutants[0].Type, s.Level)
		}
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	// When the mutant is active it returns nil without evaluating the expression, and otherwise the value received as an error. The format site is rewritten inside it.
	if !strings.Contains(out, "func() error { switch gamutActive { case 1: return nil }; var gamutC error = fmt.Errorf(func() string {") {
		t.Errorf("the returned error does not wrap its inner site:\n%s", out)
	}
}

func TestBuild_ConditionsWrapTheirInnerSites(t *testing.T) {
	dir := t.TempDir()
	src := "package m\n\nfunc F(a, b int) int {\n\tif a < b {\n\t\treturn 1\n\t}\n\treturn 0\n}\n"
	for name, s := range map[string]string{"go.mod": "module m\n\ngo 1.27.4\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, []mutant.Type{mutant.ConditionsConstant, mutant.ConditionalsBoundary})
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Sites {
		if s.Level != schemata.Wrapped {
			t.Errorf("%s site: level %d, want Wrapped", s.Mutants[0].Type, s.Level)
		}
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	// Inside the function wrapping the condition, the condition expression (the boundary mutation site) is rewritten once.
	if !strings.Contains(out, "case 1: return true; case 2: return false; }; gamutC := func() bool {") || strings.Count(out, "gamutHit(2)") != 1 {
		t.Errorf("the condition does not wrap its inner site:\n%s", out)
	}
}

func TestBuild_RemovedStatementsKeepTheirArguments(t *testing.T) {
	dir := t.TempDir()
	src := "package m\n\nvar total int\n\nfunc add(n int) { total += n }\n\nfunc F(n int) {\n\tadd(n * 2)\n}\n"
	for name, s := range map[string]string{"go.mod": "module m\n\ngo 1.27.4\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, []mutant.Type{mutant.StatementRemove, mutant.ArithmeticBase})
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Sites {
		if s.Level != schemata.Wrapped {
			t.Errorf("%s site: level %d, want Wrapped", s.Mutants[0].Type, s.Level)
		}
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	// The call runs unless the mutant is active, and mutation sites in the arguments are rewritten inside it.
	if !strings.Contains(out, "if gamutActive != 1 { add(func(") {
		t.Errorf("the removed statement does not wrap its arguments' site:\n%s", out)
	}
}

func TestBuild_ReturnZeroKeepsTheEntryRecord(t *testing.T) {
	dir := t.TempDir()
	src := "package m\n\nfunc F(a int) (int, error) {\n\treturn a + 1, nil\n}\n"
	for name, s := range map[string]string{"go.mod": "module m\n\ngo 1.27.4\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, []mutant.Type{mutant.ReturnZero, mutant.ArithmeticBase})
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Sites {
		if s.Level != schemata.Wrapped {
			t.Errorf("%s site: level %d, want Wrapped", s.Mutants[0].Type, s.Level)
		}
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	// The function entry record and the branch returning zero values are both right after the body's {. Lines do not change.
	if !strings.Contains(out, "(int, error) { if gamutCovering { gamutEnter(1) }; if gamutCovering { gamutHit(1); gamutInfect(1) }; if gamutActive == 1 { return *new(int), *new(error) };") {
		t.Errorf("the entry record or the zero return is missing:\n%s", out)
	}
	if strings.Count(out, "\n") != strings.Count(src, "\n") {
		t.Errorf("the rewrite shifted lines:\n%s", out)
	}
}

func TestBuild_PureOperandsNeedNoTypeName(t *testing.T) {
	// *url.URL cannot be written because this file does not import net/url. Copying the operands into the body needs no type.
	res := build(t, "package m\n\nimport \"net/http\"\n\nfunc F(a, b *http.Request) bool { return a.URL == b.URL }\n")
	if got := levels(res)["CONDITIONALS_NEGATION =="]; got != schemata.Wrapped {
		t.Fatalf("level = %d, want Wrapped", got)
	}
}

func TestBuild_UnnameableTypeIsMarked(t *testing.T) {
	// The call's result cannot be copied and the type *url.URL cannot be written, so only reach is recorded.
	res := build(t, "package m\n\nimport \"net/http\"\n\nfunc u(r *http.Request) any { return r.URL }\n\nfunc F(r *http.Request) bool { return u(r) == r.URL }\n")
	if got := levels(res)["CONDITIONALS_NEGATION =="]; got != schemata.Wrapped {
		t.Fatalf("level = %d, want Wrapped (any is nameable)", got)
	}
	res = build(t, "package m\n\nimport \"net/http\"\n\nfunc u(r *http.Request) *http.Request { return r }\n\nfunc F(r *http.Request) bool { return u(r).URL == u(r).URL }\n")
	if got := levels(res)["CONDITIONALS_NEGATION =="]; got != schemata.Marked {
		t.Fatalf("level = %d, want Marked", got)
	}
}

func TestBuild_ConstantContextIsUntouched(t *testing.T) {
	res := build(t, "package m\n\nconst N = 2 * 3\n\nvar A [N - 1]int\n")
	for key, level := range levels(res) {
		if level != schemata.Untouched {
			t.Errorf("%s: level = %d, want Untouched", key, level)
		}
	}
	// Without function declarations, no entry records are inserted either.
	if len(res.Files) != 0 {
		t.Errorf("no file should be rewritten, got %d", len(res.Files))
	}
}

func TestBuild_RecordsFunctionEntriesWithoutShiftingLines(t *testing.T) {
	const src = "package m\n\nfunc F(a int) int {\n\treturn a + 1\n}\n\nfunc G() {}\n\n//go:nosplit\nfunc H() {}\n\nfunc (T) M() {}\n\ntype T struct{}\n"
	res := build(t, src)
	var names []string
	for _, fd := range res.Funcs {
		names = append(names, fd.Name.Name)
	}
	// Nothing is inserted into //go:nosplit functions.
	if got := strings.Join(names, " "); got != "F G M" {
		t.Fatalf("Funcs = %s, want F G M", got)
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	want := []string{
		"func F(a int) int { if gamutCovering { gamutEnter(1) };\n",
		"func G() { if gamutCovering { gamutEnter(2) };}",
		"func H() {}",
		"func (T) M() { if gamutCovering { gamutEnter(3) };}",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("rewritten source lacks %q:\n%s", w, out)
		}
	}
	if strings.Count(out, "\n") != strings.Count(src, "\n") {
		t.Errorf("the rewrite changed the number of lines:\n%s", out)
	}
}

func TestBuild_RejectsHelperNameCollision(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.27.4\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "m.go"), []byte("package m\n\nvar gamutActive = 1\n"), 0o600)
	pkg, err := loadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schemata.Build(pkg, nil); err == nil {
		t.Fatal("want an error for a colliding helper name")
	}
}

// loadOne loads the one package in dir.
func loadOne(dir string) (*load.Package, error) {
	pkgs, err := load.Load([]string{dir}, nil)
	if err != nil {
		return nil, err
	}
	return pkgs[0], nil
}
