package main

import (
	"strings"
	"testing"
)

// TestSimplifyReporter checks that survivors of removals are listed as candidates per enclosing function, and detected mutants are not.
func TestSimplifyReporter(t *testing.T) {
	var stdout, stderr strings.Builder
	args := []string{"-no-cache", "-only", "STATEMENT_REMOVE,CONDITIONS_CONSTANT,ARITHMETIC_BASE", "-reporter", "simplify", "../../testdata/simplify"}
	if code := realMain(args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"simplify.go: Abs",
		"10:2 STATEMENT_REMOVE `touch()` removed",
		"14:5 CONDITIONS_CONSTANT `n == 0` -> `false`",
		"1 more removals survive in code no test reaches",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the candidates lack %q:\n%s", want, out)
		}
	}
	// Making n < 0 constant and n == 0 -> true are detected, and ARITHMETIC_BASE is not a removal.
	for _, unwanted := range []string{"`n < 0`", "`n == 0` -> `true`", "ARITHMETIC_BASE", "killed"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("the candidates contain %q:\n%s", unwanted, out)
		}
	}
	if code := realMain([]string{"-no-cache", "-reporter", "simplify", "-reporter", "agent", "../../testdata/simplify"}, &stdout, &stderr); code != exitError {
		t.Errorf("simplify with agent: exit %d, want %d", code, exitError)
	}
}
