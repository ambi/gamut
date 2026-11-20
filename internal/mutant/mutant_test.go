package mutant_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
)

func writeModule(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.27.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func enumerate(t *testing.T, src string) []*mutant.Mutant {
	t.Helper()
	pkg, err := loadOne(writeModule(t, src), nil)
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, mutant.AllTypes)
	mutant.CheckViability(pkg, ms)
	return ms
}

func TestEnumerate_IDsDoNotDependOnLinesAndColumns(t *testing.T) {
	before := enumerate(t, "package m\n\nfunc F(a, b int) bool { return a < b }\n")
	after := enumerate(t, "package m\n\n// 説明を足しても識別子は変わらない。\nvar X = 1\n\nfunc F(a, b int) bool {\n\treturn a < b\n}\n")
	if len(before) != 2 || len(after) != 2 {
		t.Fatalf("got %d and %d mutants, want 2 each", len(before), len(after))
	}
	for i := range before {
		if before[i].ID != after[i].ID {
			t.Errorf("ID changed: %s -> %s", before[i].ID, after[i].ID)
		}
		if before[i].Line == after[i].Line {
			t.Errorf("fixture must move the line")
		}
	}
}

func TestEnumerate_IDsDistinguishSiblings(t *testing.T) {
	ms := enumerate(t, "package m\n\nfunc F(a, b int) int { return a + b + a }\n")
	seen := map[string]bool{}
	for _, m := range ms {
		if seen[m.ID] {
			t.Fatalf("duplicate ID %s", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestEnumerate_Operators(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string // "TYPE from->to viable"
	}{
		{"string concatenation is not viable", "package m\n\nfunc F(s string) string { return s + \"x\" }\n",
			[]string{"ARITHMETIC_BASE +->- false"}},
		{"integer division by constant zero is not viable", "package m\n\nfunc F(a int) int { return a * 0 }\n",
			[]string{"ARITHMETIC_BASE *->/ false"}},
		{"remainder", "package m\n\nfunc F(a int) int { return a % 3 }\n",
			[]string{"ARITHMETIC_BASE %->* true"}},
		{"equality", "package m\n\nfunc F(a int) bool { return a == 3 }\n",
			[]string{"CONDITIONALS_NEGATION ==->!= true"}},
		{"unary minus only", "package m\n\nfunc F(a int) int { return -a - 1 }\n",
			[]string{"ARITHMETIC_BASE -->+ true", "INVERT_NEGATIVES -->+ true"}},
		{"decrement", "package m\n\nfunc F(a int) int { a--; return a }\n",
			[]string{"INCREMENT_DECREMENT --->++ true"}},
		{"log arguments are arid", "package m\n\nimport \"log\"\n\nfunc F(a int) { log.Println(a + 1) }\n",
			nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, m := range enumerate(t, tt.src) {
				got = append(got, string(m.Type)+" "+m.Original.String()+"->"+m.Replacement.String()+" "+map[bool]string{true: "true", false: "false"}[m.Viable])
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// loadOne は、dir の 1 パッケージを読む。
func loadOne(dir string, tags []string) (*load.Package, error) {
	pkgs, err := load.Load([]string{dir}, tags)
	if err != nil {
		return nil, err
	}
	return pkgs[0], nil
}
