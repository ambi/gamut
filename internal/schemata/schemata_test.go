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
	ms := mutant.Enumerate(pkg, mutant.AllTypes)
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
	ms := mutant.Enumerate(pkg, []mutant.Type{mutant.InvertAssignments, mutant.InvertBitwiseAssignments})
	mutant.CheckViability(pkg, ms)
	res, err := schemata.Build(pkg, ms)
	if err != nil {
		t.Fatal(err)
	}
	got := levels(res)
	for _, key := range []string{"INVERT_ASSIGNMENTS +=", "INVERT_BITWISE_ASSIGNMENTS <<="} {
		if got[key] != schemata.Wrapped {
			t.Errorf("%s: level %d, want Wrapped (levels %v)", key, got[key], got)
		}
	}
	var out string
	for _, b := range res.Files {
		out = string(b)
	}
	// 右辺の呼び出しは引数として 1 回だけ書き、本文では引数の名前で参照する。
	if strings.Count(out, "}(next())") != 1 || strings.Contains(out, "= next()") || !strings.Contains(out, "a -= gamutV") {
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
