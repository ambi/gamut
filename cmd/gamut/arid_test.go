package main

import (
	"path/filepath"
	"testing"
)

// TestArid_FlagsAndCount は、arid の既定の規則、-arid-call、-arid-package、-no-arid と、報告の arid の数を確かめる。
func TestArid_FlagsAndCount(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module ar\n\ngo 1.27.4\n")
	write(t, filepath.Join(dir, "audit"), "audit.go", "package audit\n\nfunc Record(n int) {}\n")
	write(t, dir, "ar.go", `package ar

import (
	"log"

	"ar/audit"
)

func F(n int) int {
	log.Println(n + 1)
	audit.Record(n - 1)
	return n * 2
}
`)
	write(t, dir, "ar_test.go", "package ar\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) {\n\tif F(2) != 4 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n")
	types := func(args ...string) (map[string]int, int) {
		t.Helper()
		reportPath := filepath.Join(t.TempDir(), "r.json")
		if code, stderr := exit(t, append(append([]string{"-no-cache", "-o", reportPath}, args...), dir)...); code != 0 {
			t.Fatalf("exit %d:\n%s", code, stderr)
		}
		rep := readMTE(t, reportPath)
		got := map[string]int{}
		for _, m := range rep.mutants() {
			got[m.Type]++
		}
		return got, rep.Gamut.Counts.Arid
	}
	// 既定：log.Println の引数（+）と文の削除を除く。audit.Record の引数（-）と文の削除、本体の * と RETURN_ZERO は残る。
	got, arid := types()
	if got["ARITHMETIC_BASE"] != 2 || got["STATEMENT_REMOVE"] != 1 || arid != 2 {
		t.Errorf("default: %v, arid %d; want 2 arithmetic, 1 statement removal, 2 arid", got, arid)
	}
	for _, args := range [][]string{{"-arid-call", "ar/audit.Record"}, {"-arid-package", "ar/audit"}} {
		got, arid := types(args...)
		if got["ARITHMETIC_BASE"] != 1 || got["STATEMENT_REMOVE"] != 0 || arid != 4 {
			t.Errorf("%v: %v, arid %d; want 1 arithmetic, no statement removal, 4 arid", args, got, arid)
		}
	}
	got, arid = types("-no-arid")
	if got["ARITHMETIC_BASE"] != 3 || got["STATEMENT_REMOVE"] != 2 || arid != 0 {
		t.Errorf("-no-arid: %v, arid %d; want 3 arithmetic, 2 statement removals, 0 arid", got, arid)
	}
}
