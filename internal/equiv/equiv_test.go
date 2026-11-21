package equiv_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambi/gamut/internal/equiv"
	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
)

func TestDetector_ComparesCompiledCode(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod": "module eq\n\ngo 1.27.4\n",
		"eq.go": `package eq

import "strconv"

// Scale は x * 1 と x / 1 が同じコードになる。
func Scale(x int) int { return x * 1 }

// Shift は x + 0 と x - 0 が同じコードになる。
func Shift(x int) int { return x + 0 }

// Name は x + 2 と x - 2 が違うコードになる。
func Name(x int) string { return strconv.Itoa(x + 2) }
`,
		"other.go": "package eq\n\nfunc Other(a, b int) bool { return a < b }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkgs, err := load.Load([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pkg := pkgs[0]
	if !pkg.Direct {
		t.Fatal("a plain package is not directly compilable")
	}
	d, err := equiv.New(context.Background(), pkg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"Scale ARITHMETIC_BASE": true,
		"Shift ARITHMETIC_BASE": true,
		"Name ARITHMETIC_BASE":  false,
	}
	seen := 0
	for _, m := range mutant.Enumerate(pkg, classic) {
		w, ok := want[m.Scope+" "+string(m.Type)]
		if !ok {
			continue
		}
		seen++
		got, err := d.Equivalent(context.Background(), m)
		if err != nil {
			t.Fatal(err)
		}
		if got != w {
			t.Errorf("%s %s -> %s: equivalent %t, want %t", m.Scope, m.Original, m.Replacement, got, w)
		}
	}
	if seen != len(want) {
		t.Fatalf("saw %d of %d mutants", seen, len(want))
	}
}

func TestDetector_RefusesPackagesThatNeedMoreThanGoFiles(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":   "module em\n\ngo 1.27.4\n",
		"em.go":    "package em\n\nimport _ \"embed\"\n\n//go:embed data.txt\nvar data string\n\nfunc Len() int { return len(data) + 1 }\n",
		"data.txt": "x\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkgs, err := load.Load([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pkgs[0].Direct {
		t.Fatal("a package with embedded files is marked directly compilable")
	}
	if _, err := equiv.New(context.Background(), pkgs[0], t.TempDir()); err == nil {
		t.Error("New succeeded for a package with embedded files")
	}
}
