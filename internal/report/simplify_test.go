package report

import (
	"strings"
	"testing"
)

// TestWriteSimplify は、候補の選び方（検出されなかった削除の変異だけ）と、複数行の文の表示を確かめる。
func TestWriteSimplify(t *testing.T) {
	rep := &Report{Files: []File{{Path: "a.go", Mutations: []Mutation{
		{ID: "a.go::STATEMENT_REMOVE::F::1", Type: "STATEMENT_REMOVE", Status: "LIVED", Reason: "not detected", Line: 3, Column: 2, Original: "save(x,\n\ty)", Replacement: "<removed>"},
		{ID: "a.go::CONDITIONS_CONSTANT::T.M::2", Type: "CONDITIONS_CONSTANT", Status: "LIVED", Reason: "not detected", Line: 9, Column: 5, Original: "ok", Replacement: "true"},
		{ID: "a.go::CONDITIONS_CONSTANT::F::3", Type: "CONDITIONS_CONSTANT", Status: "LIVED", Reason: "not infected", Line: 4, Column: 5, Original: "err != nil", Replacement: "false"},
		{ID: "a.go::STATEMENT_REMOVE::G::4", Type: "STATEMENT_REMOVE", Status: "NOT COVERED", Line: 20, Column: 2, Original: "g()", Replacement: "<removed>"},
		{ID: "a.go::STATEMENT_REMOVE::F::5", Type: "STATEMENT_REMOVE", Status: "LIVED", Reason: "not detected", Accepted: "logging", Original: "log()"},
		{ID: "a.go::STATEMENT_REMOVE::F::6", Type: "STATEMENT_REMOVE", Status: "KILLED", Original: "must()"},
		{ID: "a.go::ARITHMETIC_BASE::F::7", Type: "ARITHMETIC_BASE", Status: "LIVED", Reason: "not detected", Original: "+", Replacement: "-"},
	}}}}
	var out strings.Builder
	WriteSimplify(&out, rep)
	got := out.String()
	for _, want := range []string{
		"simplification candidates (2 in 2 functions)",
		"a.go: F\n  3:2 STATEMENT_REMOVE `save(x, …` removed  [a.go::STATEMENT_REMOVE::F::1]",
		"a.go: T.M\n  9:5 CONDITIONS_CONSTANT `ok` -> `true`",
		"1 more removals survive because no test input makes them differ",
		"1 more removals survive in code no test reaches",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"log()", "must()", "ARITHMETIC_BASE", "err != nil", "g()"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("output contains %q:\n%s", unwanted, got)
		}
	}
}
