package report_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/report"
)

func TestAgent_DescribesEachSurvivor(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\n// Clamp limits v to max.\nfunc Clamp(v, max int) int {\n\tif v > max {\n\t\treturn max\n\t}\n\treturn v\n}\n\nfunc Unused(a int) int { return a * 2 }\n"
	test := `package p

import (
	"os"
	"os/exec"
	"testing"
)

func TestClampBelow(t *testing.T) { _ = Clamp(1, 5) }

func TestOther(t *testing.T) {}

func TestSlow(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
}

func TestChild(t *testing.T) { _ = exec.Command(os.Args[0], "-test.run=TestX") }
`
	for name, s := range map[string]string{"p.go": src, "p_test.go": test} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var tests []string
	for i := range 12 {
		tests = append(tests, fmt.Sprintf("TestOther%02d", i))
	}
	tests = append(tests, "TestClampBelow")
	boundary := report.Mutation{ID: "p.go::CONDITIONALS_BOUNDARY::Clamp::01", Type: "CONDITIONALS_BOUNDARY", Status: "LIVED", Reason: "not infected",
		Line: 5, Column: 7, Original: ">", Replacement: ">=", Tests: tests, Critical: true}
	rep := &report.Report{Package: "p", MutationScore: 50, Files: []report.File{{Path: "p.go", Mutations: []report.Mutation{
		boundary,
		{ID: "p.go::CONDITIONALS_NEGATION::Clamp::02", Type: "CONDITIONALS_NEGATION", Status: "KILLED", Line: 5, Column: 7, Original: ">", Replacement: "<="},
		{ID: "p.go::ARITHMETIC_BASE::Unused::03", Type: "ARITHMETIC_BASE", Status: "NOT COVERED", Line: 11, Column: 34, Original: "*", Replacement: "/"},
	}}}}
	out, err := rep.Agent(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Survivors) != 2 {
		t.Fatalf("survivors %+v, want 2", out.Survivors)
	}
	s := out.Survivors[0]
	if s.ID != boundary.ID || s.Class != "not infected" || s.File != "p.go" || s.Line != 5 || !s.Critical {
		t.Errorf("survivor %+v", s)
	}
	if s.Diff.Before != "\tif v > max {" || s.Diff.After != "\tif v >= max {" {
		t.Errorf("diff %+v", s.Diff)
	}
	if !strings.HasPrefix(s.Declaration, "// Clamp limits") || !strings.HasSuffix(s.Declaration, "return v\n}") {
		t.Errorf("declaration %q, want the function with its doc comment", s.Declaration)
	}
	// Tests containing the declaration's name come first, up to 10, with the total.
	if s.TestsTotal != 13 || len(s.Tests) != 10 || s.Tests[0] != "TestClampBelow" {
		t.Errorf("tests %v (total %d), want TestClampBelow first, 10 shown of 13", s.Tests, s.TestsTotal)
	}
	if !strings.Contains(s.Hint, "equal") {
		t.Errorf("boundary hint %q, want advice on the equal case", s.Hint)
	}
	if u := out.Survivors[1]; u.Class != "not covered" || !strings.Contains(u.Hint, "Unused") || u.TestsTotal != 0 {
		t.Errorf("not covered survivor %+v", u)
	}
	var warnings string
	for _, w := range out.Warnings {
		warnings += w + "\n"
	}
	if !strings.Contains(warnings, "TestSlow") || !strings.Contains(warnings, "TestChild") || strings.Contains(warnings, "TestOther") {
		t.Errorf("warnings:\n%s\nwant TestSlow (skip) and TestChild (child process) only", warnings)
	}
}
