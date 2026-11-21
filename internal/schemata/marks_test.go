package schemata_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/schemata"
)

func TestMarkTests_InsertsBoundariesWithoutShiftingLines(t *testing.T) {
	cases := []struct {
		name      string
		file      string
		src       string
		want      []string // text the rewrite contains
		batchable []string
	}{
		{name: "internal test", file: "m_test.go",
			src:       "package m\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\t_ = 1\n}\n\nfunc helper() {}\n\nfunc TestMain(m *testing.M) { m.Run() }\n",
			want:      []string{`func TestA(t *testing.T) { GamutTestStart("TestA");`, "func helper() {}", "func TestMain(m *testing.M) { m.Run() }"},
			batchable: []string{"TestA"}},
		{name: "external test calls the exported helper", file: "x_test.go",
			src:       "package m_test\n\nimport (\n\t\"testing\"\n\n\tmm \"m\"\n)\n\nfunc ExampleF() { mm.F() }\n",
			want:      []string{`func ExampleF() { mm.GamutTestStart("ExampleF");`},
			batchable: []string{"ExampleF"}},
		{name: "a parallel test is marked again where it resumes", file: "p_test.go",
			src:       "package m\n\nimport \"testing\"\n\nfunc TestP(t *testing.T) {\n\tt.Parallel()\n}\n",
			want:      []string{`t.Parallel(); GamutTestStart("TestP");`},
			batchable: []string{"TestP"}},
		{name: "a parallel test with parallel subtests is measured alone", file: "q_test.go",
			src:  "package m\n\nimport \"testing\"\n\nfunc TestQ(t *testing.T) {\n\tt.Parallel()\n\tt.Run(\"s\", func(t *testing.T) { t.Parallel() })\n}\n",
			want: []string{`GamutTestStart("TestQ");`}},
		{name: "a sequential test with parallel subtests is batched", file: "r_test.go",
			src:       "package m\n\nimport \"testing\"\n\nfunc TestR(t *testing.T) {\n\tt.Run(\"s\", func(t *testing.T) { t.Parallel() })\n}\n",
			want:      []string{`func TestR(t *testing.T) { GamutTestStart("TestR");`},
			batchable: []string{"TestR"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, src := range map[string]string{"go.mod": "module m\n\ngo 1.27.4\n", "m.go": "package m\n\nfunc F() {}\n", tc.file: tc.src} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			pkg, err := loadOne(dir)
			if err != nil {
				t.Fatal(err)
			}
			marks, err := schemata.MarkTests(pkg)
			if err != nil {
				t.Fatal(err)
			}
			out := string(marks.Files[filepath.Join(pkg.Dir, tc.file)])
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("rewritten test lacks %q:\n%s", w, out)
				}
			}
			if strings.Count(out, "\n") != strings.Count(tc.src, "\n") {
				t.Errorf("the rewrite changed the number of lines:\n%s", out)
			}
			var got []string
			for name := range marks.Batchable {
				got = append(got, name)
			}
			if strings.Join(got, ",") != strings.Join(tc.batchable, ",") {
				t.Errorf("Batchable = %v, want %v", got, tc.batchable)
			}
		})
	}
}
