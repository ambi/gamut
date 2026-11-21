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
	// 外側の比較は内側の加算の書き換え結果を 1 回だけ引数に取り、内側を本文へ写さない。
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
	// 右辺の呼び出しは引数として 1 回だけ書き、本文では引数の名前で参照する。
	if strings.Count(out, "}(next())") != 1 || strings.Contains(out, "= next()") || !strings.Contains(out, "a -= gamutV") || !strings.Contains(out, "a = gamutV") {
		t.Errorf("the right-hand side must be evaluated once and passed in:\n%s", out)
	}
	// 書き換えても行は変わらない。
	if strings.Count(out, "\n") != strings.Count(src, "\n") {
		t.Errorf("the rewrite shifted lines:\n%s", out)
	}
}

func TestBuild_LoopControlBecomesTwoBranches(t *testing.T) {
	dir := t.TempDir()
	// G の for は break がないので終端する。continue を break にすると終端しなくなり、missing return になる。
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
	// 右辺の呼び出しは、元の形で左辺が偽なら評価されない。右辺の中の比較も変異箇所である。
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
	// 右辺は gamutR の本文に 1 回だけ現れ、呼ぶのは && と || の右側だけである。
	if strings.Count(out, "expensive(n)") != 1 || !strings.Contains(out, "return (a) || gamutR()") || !strings.Contains(out, "return (a) && gamutR()") {
		t.Errorf("the right operand must stay lazy and appear once:\n%s", out)
	}
}

func TestBuild_BooleanLiteralsTakeTheirContextType(t *testing.T) {
	dir := t.TempDir()
	// Flag は名前付きの bool である。const の宣言の中の true は、呼び出しにできないので個別ビルドに回る。
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
	// const の宣言と配列の長さのリテラルは、定数でなければならないので個別ビルドに回る。
	// M の 10 を呼び出しにすると、定数でないという誤りはリテラルの外（64 の位置）に出るので、書き換える前に個別ビルドに回す。
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
	// M の 64 と 10 で 2 か所。
	if len(res.Sites) != 6 {
		t.Errorf("%d sites, want 6", len(res.Sites))
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	// 2 は float64 の文脈にあるので、float64 を返す呼び出しになる。
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
	// 本体は有効な変異体でなければ実行し、本体の中の変異箇所はその中で書き換える。
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
	// 有効な変異体なら式を評価せずに nil を返し、そうでなければ error として受けた値を返す。書式の箇所はその中で書き換える。
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
	// 条件を包む関数の中で、条件の式（境界の変異箇所）を 1 回だけ書き換える。
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
	// 呼び出しは有効な変異体でなければ実行し、引数の中の変異箇所はその中で書き換える。
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
	// 関数の入口の記録と、ゼロ値を返す分岐が、どちらも本体の { の直後にある。行は変わらない。
	if !strings.Contains(out, "(int, error) { if gamutCovering { gamutEnter(1) }; if gamutCovering { gamutHit(1); gamutInfect(1) }; if gamutActive == 1 { return *new(int), *new(error) };") {
		t.Errorf("the entry record or the zero return is missing:\n%s", out)
	}
	if strings.Count(out, "\n") != strings.Count(src, "\n") {
		t.Errorf("the rewrite shifted lines:\n%s", out)
	}
}

func TestBuild_PureOperandsNeedNoTypeName(t *testing.T) {
	// *url.URL は、このファイルが net/url を import しないので書けない。被演算子を本文へ写せば型は要らない。
	res := build(t, "package m\n\nimport \"net/http\"\n\nfunc F(a, b *http.Request) bool { return a.URL == b.URL }\n")
	if got := levels(res)["CONDITIONALS_NEGATION =="]; got != schemata.Wrapped {
		t.Fatalf("level = %d, want Wrapped", got)
	}
}

func TestBuild_UnnameableTypeIsMarked(t *testing.T) {
	// 呼び出しの結果は写せず、型 *url.URL も書けないので、到達だけを記録する。
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
	// 関数の宣言がなければ、入口の記録も差し込まない。
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
	// //go:nosplit の関数には差し込まない。
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

// loadOne は、dir の 1 パッケージを読む。
func loadOne(dir string) (*load.Package, error) {
	pkgs, err := load.Load([]string{dir}, nil)
	if err != nil {
		return nil, err
	}
	return pkgs[0], nil
}
