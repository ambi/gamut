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
	pkg, err := load.Load(dir)
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
	if len(res.Files) != 0 {
		t.Errorf("no file should be rewritten, got %d", len(res.Files))
	}
}

func TestBuild_RejectsHelperNameCollision(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.27.4\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "m.go"), []byte("package m\n\nvar gamutActive = 1\n"), 0o600)
	pkg, err := load.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schemata.Build(pkg, nil); err == nil {
		t.Fatal("want an error for a colliding helper name")
	}
}
