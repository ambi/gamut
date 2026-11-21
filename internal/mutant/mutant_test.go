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

// everywhere は、ほとんどの条件、関数、リテラル、分岐、複合代入に現れる変異演算子である。それを確かめる例でだけ数える。
var everywhere = []mutant.Type{mutant.ConditionsConstant, mutant.ReturnZero, mutant.RemoveSelfAssignments,
	mutant.IntegerIncrement, mutant.IntegerDecrement, mutant.FloatIncrement, mutant.FloatDecrement,
	mutant.BranchIf, mutant.BranchElse, mutant.BranchCase}

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
	// 関数の外に置き、既定の RETURN_ZERO と STATEMENT_REMOVE の箇所を作らない。
	pkg, err := loadOne(writeModule(t, "package m\n\nvar a, b = 1, 2\n\nvar X = a | b\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ms := mutant.Enumerate(pkg, mutant.AllTypes); len(ms) != 0 {
		t.Errorf("default operators enumerate %d mutants for a | b, want 0", len(ms))
	}
}

func TestEnumerate_IDsDoNotDependOnLinesAndColumns(t *testing.T) {
	before := enumerate(t, "package m\n\nfunc F(a, b int) bool { return a < b }\n")
	after := enumerate(t, "package m\n\n// 説明を足しても識別子は変わらない。\nvar X = \"x\"\n\nfunc F(a, b int) bool {\n\treturn a < b\n}\n")
	// 比較の 2 体と、関数の本体の RETURN_ZERO の 1 体。RETURN_ZERO の識別子も、行と列によらない。
	if len(before) != 3 || len(after) != 3 {
		t.Fatalf("got %d and %d mutants, want 3 each", len(before), len(after))
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
		{"calls without results are removed", "package m\n\nfunc g(n int) {}\n\nfunc h() int { return 1 }\n\nfunc F(n int) {\n\tg(n + 1)\n\t_ = h()\n\th()\n}\n",
			[]string{"STATEMENT_REMOVE g(n + 1)->if false { g(n + 1) } true", "ARITHMETIC_BASE +->- true"}},
		{"log calls are arid", "package m\n\nimport \"log\"\n\nfunc F(n int) { log.Println(n) }\n",
			nil},
		{"functions with results return zero values", "package m\n\ntype T struct{}\n\nfunc (T) Name() string { return \"t\" }\n\nfunc F(a int) (n, m int, err error) { return a, a, nil }\n\nfunc G[E any](e E) E { return e }\n\nfunc init() {}\n\nfunc H() {}\n",
			[]string{"RETURN_ZERO {->{ return *new(string); true", "RETURN_ZERO {->{ return *new(int), *new(int), *new(error); true", "RETURN_ZERO {->{ return *new(E); true"}},
		{"compound assignments drop the operator", "package m\n\nfunc F(a, b int, s string) int { a += b; a <<= 1; s += \"x\"; return a + len(s) }\n",
			[]string{"INVERT_ASSIGNMENTS +=->-= true", "REMOVE_SELF_ASSIGNMENTS +=->= true", "INVERT_BITWISE_ASSIGNMENTS <<=->>>= true", "REMOVE_SELF_ASSIGNMENTS <<=->= true",
				"INVERT_ASSIGNMENTS +=->-= false", "REMOVE_SELF_ASSIGNMENTS +=->= true", "ARITHMETIC_BASE +->- true"}},
		{"a shift by another type is not viable as a plain assignment", "package m\n\nfunc F(u uint, n int) uint { u <<= n; return u }\n",
			[]string{"INVERT_BITWISE_ASSIGNMENTS <<=->>>= true", "REMOVE_SELF_ASSIGNMENTS <<=->= false"}},
		{"integer literals move by one", "package m\n\nfunc F(a int) int { return a + 41 }\n",
			[]string{"ARITHMETIC_BASE +->- true", "INTEGER_INCREMENT 41->42 true", "INTEGER_DECREMENT 41->40 true"}},
		{"non-decimal integer literals become decimal", "package m\n\nfunc F(a int) int { return a & 0xFF }\n",
			[]string{"INVERT_BITWISE &->| true", "INTEGER_INCREMENT 0xFF->256 true", "INTEGER_DECREMENT 0xFF->254 true"}},
		{"negative results are parenthesized and may overflow", "package m\n\nfunc F() uint {\n\tvar u uint = 0\n\treturn u\n}\n",
			[]string{"INTEGER_INCREMENT 0->1 true", "INTEGER_DECREMENT 0->(-1) false"}},
		{"float literals move by one and stay floats", "package m\n\nfunc F(x float64) float64 { return x*1.5 + 1.0 + 1e2 }\n",
			[]string{"ARITHMETIC_BASE +->- true", "ARITHMETIC_BASE +->- true", "ARITHMETIC_BASE *->/ true",
				"FLOAT_INCREMENT 1.5->2.5 true", "FLOAT_DECREMENT 1.5->0.5 true", "FLOAT_INCREMENT 1.0->2.0 true", "FLOAT_DECREMENT 1.0->0.0 true",
				"FLOAT_INCREMENT 1e2->101.0 true", "FLOAT_DECREMENT 1e2->99.0 true"}},
		{"capacities and size hints of make are not mutated", "package m\n\nfunc F(n int) ([]int, map[int]int, []int) {\n\treturn make([]int, n, 8), make(map[int]int, 4), make([]int, 3)\n}\n",
			[]string{"INTEGER_INCREMENT 3->4 true", "INTEGER_DECREMENT 3->2 true"}},
		{"if and else bodies are emptied, keeping their references", "package m\n\nfunc F(a int) int {\n\tn := 0\n\tif a > 0 {\n\t\tn = a\n\t} else if a < -9 {\n\t\tn = -a\n\t} else {\n\t\tn = 1\n\t}\n\treturn n\n}\n",
			[]string{"CONDITIONALS_BOUNDARY >->>= true", "CONDITIONALS_NEGATION >-><= true", "BRANCH_IF \n\t\tn = a\n\t->if false {\n\t\tn = a\n\t} true",
				"CONDITIONALS_BOUNDARY <-><= true", "CONDITIONALS_NEGATION <->>= true", "INVERT_NEGATIVES -->+ true",
				"BRANCH_IF \n\t\tn = -a\n\t->if false {\n\t\tn = -a\n\t} true", "INVERT_NEGATIVES -->+ true", "BRANCH_ELSE \n\t\tn = 1\n\t->if false {\n\t\tn = 1\n\t} true"}},
		{"a body the function needs to end is not viable when emptied", "package m\n\nfunc F(a int) int {\n\tif a > 0 {\n\t\treturn 1\n\t} else {\n\t\treturn 2\n\t}\n}\n",
			[]string{"CONDITIONALS_BOUNDARY >->>= true", "CONDITIONALS_NEGATION >-><= true", "BRANCH_IF \n\t\treturn 1\n\t->if false {\n\t\treturn 1\n\t} false", "BRANCH_ELSE \n\t\treturn 2\n\t->if false {\n\t\treturn 2\n\t} false"}},
		{"case bodies are emptied, but not empty ones, logging ones, or ones that fall through", "package m\n\nimport \"log\"\n\nfunc F(a int) int {\n\tn := 0\n\tswitch a {\n\tcase 1:\n\t\tn = 1\n\tcase 2:\n\tcase 3:\n\t\tlog.Println(a)\n\tcase 4:\n\t\tn = 4\n\t\tfallthrough\n\tdefault:\n\t\tn++\n\t}\n\treturn n\n}\n",
			[]string{"BRANCH_CASE \n\t\tn = 1->if false {\n\t\tn = 1} true", "BRANCH_CASE \n\t\tn++->if false {\n\t\tn++} true", "INCREMENT_DECREMENT ++->-- true"}},
		{"type switch cases are emptied", "package m\n\nfunc F(v any) int {\n\tswitch x := v.(type) {\n\tcase int:\n\t\treturn x\n\t}\n\treturn 0\n}\n",
			[]string{"BRANCH_CASE \n\t\treturn x->if false {\n\t\treturn x} true"}},
		{"wrapping verbs of fmt.Errorf become %v", "package m\n\nimport \"fmt\"\n\nfunc F(a, b error) error {\n\t_ = fmt.Errorf(\"%w; 100%%w %s: %w\", a, \"x\", b)\n\t_ = fmt.Errorf(`raw %+w`, a)\n\t_ = fmt.Errorf(\"no wrap: %v\", a)\n\treturn nil\n}\n",
			[]string{"ERRORF_WRAP \"%w; 100%%w %s: %w\"->\"%v; 100%%w %s: %v\" true", "ERRORF_WRAP `raw %+w`->`raw %+v` true"}},
		{"fmt.Errorf inside logging is arid", "package m\n\nimport (\n\t\"fmt\"\n\t\"log\"\n)\n\nfunc F(err error) { log.Println(fmt.Errorf(\"x: %w\", err)) }\n",
			nil},
		{"returned errors become nil", "package m\n\nimport (\n\t\"errors\"\n\t\"fmt\"\n\t\"io\"\n)\n\nvar errNeg = errors.New(\"neg\")\n\nfunc F(x int) (int, error) {\n\tif x < 0 {\n\t\treturn 0, errNeg\n\t}\n\treturn x, nil\n}\n\nfunc G(err error) error { return fmt.Errorf(\"g: %w\", err) }\n\nfunc H() func() error { return func() error { return io.EOF } }\n",
			// fmt と io はその式でだけ使うので、式を参照として残さないと import が使われなくなる。
			[]string{"CONDITIONALS_BOUNDARY <-><= true", "CONDITIONALS_NEGATION <->>= true", "RETURN_ERROR_NIL errNeg->func() error { if false { return errNeg }; return nil }() true",
				"RETURN_ERROR_NIL fmt.Errorf(\"g: %w\", err)->func() error { if false { return fmt.Errorf(\"g: %w\", err) }; return nil }() true", "ERRORF_WRAP \"g: %w\"->\"g: %v\" true",
				"RETURN_ERROR_NIL io.EOF->func() error { if false { return io.EOF }; return nil }() true"}},
		{"results that are not of type error are not returned as nil", "package m\n\ntype E struct{}\n\nfunc (*E) Error() string { return \"e\" }\n\nfunc pair() (int, error) { return 1, nil }\n\nfunc F() *E { return &E{} }\n\nfunc G() (int, error) { return pair() }\n",
			nil},
		{"plain assignments are not mutated", "package m\n\nfunc F(a int) int { b := a; b = 2; return b }\n",
			nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// CONDITIONS_CONSTANT と RETURN_ZERO はほとんどの条件と関数に現れるので、それを期待しない例では数えない。
			expected := func(typ mutant.Type) bool {
				return slices.ContainsFunc(tt.want, func(w string) bool { return strings.HasPrefix(w, string(typ)+" ") })
			}
			var got []string
			for _, m := range enumerate(t, tt.src) {
				if slices.Contains(everywhere, m.Type) && !expected(m.Type) {
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
		// 条件と関数の本体を包む箇所と、リテラルの箇所は、演算子の箇所と別に確かめる。
		if slices.Contains(everywhere, m.Type) {
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
	i := slices.IndexFunc(ms, func(m *mutant.Mutant) bool { return m.Type == mutant.ArithmeticBase })
	if i < 0 {
		t.Fatal("no ARITHMETIC_BASE mutant")
	}
	m := ms[i]
	for _, other := range ms {
		// 固定の文字列で識別子を作る変異体（RETURN_ZERO など）も、自分のパスでは同じ識別子を再現する。
		if got := other.IDAt(other.File.Rel); got != other.ID {
			t.Errorf("%s: IDAt(own path) = %s", other.ID, got)
		}
	}
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
