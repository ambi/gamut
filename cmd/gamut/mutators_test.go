package main

import (
	"io"
	"strings"
	"testing"
)

func TestListMutators(t *testing.T) {
	var out strings.Builder
	if code := realMain([]string{"-list-mutators"}, &out, io.Discard); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 15 {
		t.Errorf("%d lines, want one per operator (15):\n%s", len(lines), out.String())
	}
	for _, want := range []string{"RETURN_ZERO", "default", "INVERT_LOGICAL", "opt-in", "a && b -> a || b"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the list lacks %q:\n%s", want, out.String())
		}
	}
}

func TestDisable_RemovesOperatorsFromTheDefaultSet(t *testing.T) {
	dir := twoFiles(t)
	ms, _ := mustGamut(t, "-no-cache", "-disable", "RETURN_ZERO,CONDITIONALS_NEGATION", dir)
	if len(ms) == 0 {
		t.Fatal("no mutants")
	}
	for _, m := range ms {
		if m.Type == "RETURN_ZERO" || m.Type == "CONDITIONALS_NEGATION" {
			t.Errorf("%s was disabled", m.ID)
		}
	}
	for _, args := range [][]string{
		{"-only", "ARITHMETIC_BASE", "-disable", "RETURN_ZERO"},
		{"-disable", "NO_SUCH_OPERATOR"},
	} {
		if _, stderr, code := gamut(t, append(append([]string{"-no-cache"}, args...), dir)...); code != exitError {
			t.Errorf("%v: exit %d, want %d:\n%s", args, code, exitError, stderr)
		}
	}
}
