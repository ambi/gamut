package mutant_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ambi/gamut/internal/mutant"
)

// aridModule は、src をパッケージ m の m.go に、logger をパッケージ m/zlog に置く。zlog はロガーの型のメソッドを持つ。
func aridModule(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module m\n\ngo 1.27.4\n",
		"m.go":   src,
		"zlog/zlog.go": "package zlog\n\ntype Logger struct{}\n\nfunc (Logger) Infof(string, ...any) {}\n\nfunc (Logger) Debug(...any) {}\n\n" +
			"type Store struct{}\n\nfunc (Store) Info(n int) {}\n\nfunc Emit(n int) {}\n",
	}
	for name, s := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestArid_Rules(t *testing.T) {
	cases := []struct {
		name string
		src  string
		arid *mutant.Arid
		// kept は残る変異体の「囲む宣言 演算子 元の文字列」、skipped は除いた数である。
		kept    []string
		skipped int
	}{
		{name: "logger methods and their arguments",
			src:     "package m\n\nimport \"m/zlog\"\n\nfunc F(l zlog.Logger, n int) int {\n\tl.Infof(\"n=%d\", n+1)\n\tl.Debug(n - 1)\n\treturn n * 2\n}\n",
			kept:    []string{"F ARITHMETIC_BASE *", "F RETURN_ZERO {"},
			skipped: 4}, // 2 つの引数の算術と、2 つの文の削除
		{name: "a method named Info on a non-logger type is kept",
			src:  "package m\n\nimport \"m/zlog\"\n\nfunc F(s zlog.Store, n int) {\n\ts.Info(n + 1)\n}\n",
			kept: []string{"F ARITHMETIC_BASE +", "F STATEMENT_REMOVE s.Info(n + 1)"}},
		{name: "printing to stderr, but not to stdout",
			src:     "package m\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc F(n int) {\n\tfmt.Fprintln(os.Stderr, n+1)\n\tfmt.Fprintln(os.Stdout, n-1)\n}\n",
			kept:    []string{"F ARITHMETIC_BASE -"},
			skipped: 1},
		{name: "panic and error messages keep the call but not the message",
			src:     "package m\n\nimport (\n\t\"errors\"\n\t\"fmt\"\n)\n\nfunc F(n int) error {\n\tif n < 0 {\n\t\tpanic(fmt.Sprint(n + 1))\n\t}\n\t_ = errors.New(fmt.Sprint(n * 2))\n\treturn fmt.Errorf(\"n=%d\", n-1)\n}\n",
			kept:    []string{"F CONDITIONALS_BOUNDARY <", "F CONDITIONALS_NEGATION <", "F RETURN_ZERO {", "F STATEMENT_REMOVE panic(fmt.Sprint(n + 1))"},
			skipped: 3},
		{name: "String, GoString, and Error method bodies",
			src:     "package m\n\nimport \"strconv\"\n\ntype T int\n\nfunc (t T) String() string { return strconv.Itoa(int(t) + 1) }\n\nfunc (t T) Plus() int { return int(t) + 1 }\n",
			kept:    []string{"T.Plus ARITHMETIC_BASE +", "T.Plus RETURN_ZERO {"},
			skipped: 2},
		{name: "time arithmetic",
			src:     "package m\n\nimport \"time\"\n\nfunc F(start time.Time, d time.Duration) (time.Duration, time.Duration) {\n\treturn time.Since(start) - d, d * 2\n}\n",
			kept:    []string{"F ARITHMETIC_BASE *", "F RETURN_ZERO {"},
			skipped: 1},
		{name: "an if that only logs is arid as a whole, condition included",
			src:     "package m\n\nimport \"log\"\n\nfunc F(n int) int {\n\tif n > 10 {\n\t\tlog.Println(\"big\")\n\t}\n\treturn n + 1\n}\n",
			kept:    []string{"F ARITHMETIC_BASE +", "F RETURN_ZERO {"},
			skipped: 3}, // 条件の境界と否定、ログの文の削除
		{name: "configured calls and packages",
			src:     "package m\n\nimport \"m/zlog\"\n\nfunc F(n int) {\n\tzlog.Emit(n + 1)\n}\n",
			arid:    &mutant.Arid{Calls: []string{"m/zlog.Emit"}},
			skipped: 2},
		{name: "configured packages",
			src:     "package m\n\nimport \"m/zlog\"\n\nfunc F(n int) {\n\tzlog.Emit(n + 1)\n}\n",
			arid:    &mutant.Arid{Packages: []string{"m/zlog"}},
			skipped: 2},
		{name: "arid rules turned off",
			src:  "package m\n\nimport \"log\"\n\nfunc F(n int) {\n\tlog.Println(n + 1)\n}\n",
			arid: &mutant.Arid{Off: true},
			kept: []string{"F ARITHMETIC_BASE +", "F STATEMENT_REMOVE log.Println(n + 1)"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := loadOne(aridModule(t, tc.src))
			if err != nil {
				t.Fatal(err)
			}
			arid := tc.arid
			if arid == nil {
				arid = &mutant.Arid{}
			}
			ms, skipped, err := mutant.EnumerateWith(pkg, mutant.AllTypes, arid)
			if err != nil {
				t.Fatal(err)
			}
			var kept []string
			for _, m := range ms {
				kept = append(kept, m.Scope+" "+string(m.Type)+" "+m.OriginalText)
			}
			slices.Sort(kept)
			want := slices.Sorted(slices.Values(tc.kept))
			if !slices.Equal(kept, want) {
				t.Errorf("kept %q, want %q", kept, want)
			}
			if skipped != tc.skipped {
				t.Errorf("skipped %d, want %d", skipped, tc.skipped)
			}
		})
	}
}

func TestArid_RejectsBadGlobs(t *testing.T) {
	pkg, err := loadOne(aridModule(t, "package m\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := mutant.EnumerateWith(pkg, mutant.AllTypes, &mutant.Arid{Calls: []string{"m/[x"}}); err == nil {
		t.Error("a malformed glob was accepted")
	}
}
