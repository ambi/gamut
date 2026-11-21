package mutant_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	pkg, err := loadOne(writeModule(t, src))
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, mutant.Known)
	mutant.CheckViability(pkg, ms)
	return ms
}

// TestKnown_NewOperatorsAreOffByDefault は、測ってから既定の集合を決める演算子が、既定では列挙されないことを確かめる。
func TestKnown_NewOperatorsAreOffByDefault(t *testing.T) {
	for _, typ := range mutant.Known {
		if !slices.Contains(mutant.AllTypes, typ) && !slices.Contains(mutant.OptIn, typ) {
			t.Errorf("%s is neither a default nor an opt-in operator", typ)
		}
	}
	if slices.Contains(mutant.AllTypes, mutant.InvertBitwise) {
		t.Errorf("INVERT_BITWISE is enabled by default")
	}
	pkg, err := loadOne(writeModule(t, "package m\n\nfunc F(a, b int) int { return a | b }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ms := mutant.Enumerate(pkg, mutant.AllTypes); len(ms) != 0 {
		t.Errorf("default operators enumerate %d mutants for a | b, want 0", len(ms))
	}
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
		// | と ^ は同じ優先順位なので、(a&b | a) ^ b と読まれ、外側の ^ が先に並ぶ。
		{"bitwise and, or, xor", "package m\n\nfunc F(a, b int) int { return a&b | a^b }\n",
			[]string{"INVERT_BITWISE ^->& true", "INVERT_BITWISE |->& true", "INVERT_BITWISE &->| true"}},
		{"shifts", "package m\n\nfunc F(a uint) uint { return a<<2 + a>>1 }\n",
			[]string{"ARITHMETIC_BASE +->- true", "INVERT_BITWISE <<->>> true", "INVERT_BITWISE >>-><< true"}},
		{"and not", "package m\n\nfunc F(a, b int) int { return a &^ b }\n",
			[]string{"INVERT_BITWISE &^->& true"}},
		{"arithmetic compound assignments", "package m\n\nfunc F(a, b int) int { a += b; a *= 2; a %= 3; return a }\n",
			[]string{"INVERT_ASSIGNMENTS +=->-= true", "INVERT_ASSIGNMENTS *=->/= true", "INVERT_ASSIGNMENTS %=->*= true"}},
		{"string += is not viable", "package m\n\nfunc F(s string) string { s += \"x\"; return s }\n",
			[]string{"INVERT_ASSIGNMENTS +=->-= false"}},
		{"bitwise compound assignments", "package m\n\nfunc F(a uint) uint { a |= 1; a ^= 2; a &^= 4; a <<= 1; return a }\n",
			[]string{"INVERT_BITWISE_ASSIGNMENTS |=->&= true", "INVERT_BITWISE_ASSIGNMENTS ^=->&= true", "INVERT_BITWISE_ASSIGNMENTS &^=->&= true", "INVERT_BITWISE_ASSIGNMENTS <<=->>>= true"}},
		{"loop control", "package m\n\nfunc F(xs []int) int {\n\tn := 0\n\tfor _, x := range xs {\n\t\tif x < 0 {\n\t\t\tcontinue\n\t\t}\n\t\tif x > 9 {\n\t\t\tbreak\n\t\t}\n\t\tn++\n\t}\n\treturn n\n}\n",
			[]string{"CONDITIONALS_BOUNDARY <-><= true", "CONDITIONALS_NEGATION <->>= true", "INVERT_LOOP_CTRL continue->break true",
				"CONDITIONALS_BOUNDARY >->>= true", "CONDITIONALS_NEGATION >-><= true", "INVERT_LOOP_CTRL break->continue true", "INCREMENT_DECREMENT ++->-- true"}},
		{"break in a switch outside a loop is not viable as continue", "package m\n\nfunc F(a int) int {\n\tswitch a {\n\tcase 1:\n\t\tbreak\n\t}\n\treturn a\n}\n",
			[]string{"INVERT_LOOP_CTRL break->continue false"}},
		{"logical operators", "package m\n\nfunc F(a, b, c bool) bool { return a && b || c }\n",
			[]string{"INVERT_LOGICAL ||->&& true", "INVERT_LOGICAL &&->|| true"}},
		{"logical not is removed", "package m\n\nfunc F(ok bool) bool { return !ok }\n",
			[]string{"REMOVE_LOGICAL_NOT !-> true"}},
		{"boolean literals", "package m\n\nfunc F(a int) bool {\n\tif a > 0 {\n\t\treturn true\n\t}\n\treturn false\n}\n",
			[]string{"CONDITIONALS_BOUNDARY >->>= true", "CONDITIONALS_NEGATION >-><= true", "BOOLEAN_LITERAL true->false true", "BOOLEAN_LITERAL false->true true"}},
		{"a shadowed true is not a literal", "package m\n\nfunc F() bool {\n\ttrue := false\n\treturn true\n}\n",
			[]string{"BOOLEAN_LITERAL false->true true"}},
		// 条件の変異体は、条件の式を訪れたときに、その中の変異体より先に並ぶ。
		{"if conditions become true and false", "package m\n\nfunc F(a int) int {\n\tif a > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n",
			[]string{"CONDITIONS_CONSTANT a > 0->true true", "CONDITIONS_CONSTANT a > 0->false true", "CONDITIONALS_BOUNDARY >->>= true", "CONDITIONALS_NEGATION >-><= true"}},
		{"for conditions become false only", "package m\n\nfunc F(n int) int {\n\ts := 0\n\tfor i := 0; i < n; i++ {\n\t\ts++\n\t}\n\treturn s\n}\n",
			[]string{"CONDITIONS_CONSTANT i < n->false true", "CONDITIONALS_BOUNDARY <-><= true", "CONDITIONALS_NEGATION <->>= true", "INCREMENT_DECREMENT ++->-- true", "INCREMENT_DECREMENT ++->-- true"}},
		{"operands of && become true, of || become false", "package m\n\nfunc F(a, b, c bool) bool { return (a && b) || c }\n",
			[]string{"INVERT_LOGICAL ||->&& true", "CONDITIONS_CONSTANT (a && b)->false true", "INVERT_LOGICAL &&->|| true",
				"CONDITIONS_CONSTANT a->true true", "CONDITIONS_CONSTANT b->true true", "CONDITIONS_CONSTANT c->false true"}},
		{"constant operands are not mutated", "package m\n\nfunc F(a bool) bool { return true && a }\n",
			[]string{"INVERT_LOGICAL &&->|| true", "BOOLEAN_LITERAL true->false true", "CONDITIONS_CONSTANT a->true true"}},
		{"plain assignments are not mutated", "package m\n\nfunc F(a int) int { b := a; b = 2; return b }\n",
			nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// CONDITIONS_CONSTANT はほとんどの式に現れるので、それを期待しない例では数えない。
			conditions := slices.ContainsFunc(tt.want, func(w string) bool { return strings.HasPrefix(w, string(mutant.ConditionsConstant)) })
			var got []string
			for _, m := range enumerate(t, tt.src) {
				if m.Type == mutant.ConditionsConstant && !conditions {
					continue
				}
				got = append(got, string(m.Type)+" "+m.OriginalText+"->"+m.ReplacementText+" "+map[bool]string{true: "true", false: "false"}[m.Viable])
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
func loadOne(dir string) (*load.Package, error) {
	pkgs, err := load.Load([]string{dir}, nil)
	if err != nil {
		return nil, err
	}
	return pkgs[0], nil
}

func TestEnumerate_SiteKeysGroupMutantsOfOneSite(t *testing.T) {
	before := enumerate(t, "package m\n\nfunc F(a, b int) bool { return a < b && b > 0 }\n")
	after := enumerate(t, "package m\n\n// 行をずらす。\n\nfunc F(a, b int) bool {\n\treturn a < b && b > 0\n}\n")
	sites := map[string][]string{}
	for i, m := range before {
		if m.Type == mutant.ConditionsConstant {
			continue
		}
		sites[m.SiteKey] = append(sites[m.SiteKey], m.Original.String())
		if after[i].SiteKey != m.SiteKey {
			t.Errorf("%s: site key changed with the line: %s -> %s", m.ID, m.SiteKey, after[i].SiteKey)
		}
	}
	// < と > の箇所がそれぞれ 2 体ずつ（BOUNDARY と NEGATION）、&& の箇所が 1 体（INVERT_LOGICAL）の変異体を持つ。
	if len(sites) != 3 {
		t.Fatalf("got %d site keys %v, want 3", len(sites), sites)
	}
	for key, ops := range sites {
		want := 2
		if ops[0] == "&&" {
			want = 1
		}
		if len(ops) != want || slices.ContainsFunc(ops, func(op string) bool { return op != ops[0] }) {
			t.Errorf("site %s groups %v, want %d mutants of one operator", key, ops, want)
		}
	}
}

func TestMutant_IDAtReproducesTheIDUnderAnotherPath(t *testing.T) {
	ms := enumerate(t, "package m\n\nfunc F(a, b int) int { return a + b }\n")
	m := ms[0]
	if got := m.IDAt(m.File.Rel); got != m.ID {
		t.Errorf("IDAt(own path) = %s, want %s", got, m.ID)
	}
	moved := m.IDAt("old/m.go")
	if moved == m.ID || !strings.HasPrefix(moved, "old/m.go::ARITHMETIC_BASE::F::") {
		t.Errorf("IDAt(old/m.go) = %s, want an ID under old/m.go", moved)
	}
	if !strings.HasSuffix(m.ID, "::"+m.Key()) || len(m.Key()) != 10 {
		t.Errorf("Key() = %q, want the hash part of %s", m.Key(), m.ID)
	}
}
