package report_test

import (
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/report"
)

func TestWriteGitHubAnnotations(t *testing.T) {
	m := func(id, status, reason string, line int) report.Mutation {
		return report.Mutation{ID: id, Type: "CONDITIONALS_BOUNDARY", Status: status, Reason: reason, Line: line, Column: 9, Original: "<", Replacement: "<="}
	}
	critical := m("a,b.go::CONDITIONALS_BOUNDARY::F::01", "NOT COVERED", "", 3)
	critical.Critical = true
	accepted := m("a,b.go::CONDITIONALS_BOUNDARY::F::05", "LIVED", "not detected", 7)
	accepted.Accepted = "equivalent"
	gone := m("a,b.go::CONDITIONALS_BOUNDARY::F::06", "LIVED", "not detected", 8)
	gone.PreviouslyKilledBy = "TestOld"
	rep := &report.Report{Files: []report.File{{Path: "a,b.go", Mutations: []report.Mutation{
		critical,
		m("a,b.go::CONDITIONALS_BOUNDARY::F::02", "LIVED", "not detected", 4),
		m("a,b.go::CONDITIONALS_BOUNDARY::F::03", "KILLED", "", 5),
		m("a,b.go::CONDITIONALS_BOUNDARY::F::04", "PENDING", "", 6),
		accepted,
		gone,
	}}}}
	var b strings.Builder
	report.WriteGitHubAnnotations(&b, rep, "svc")
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 annotations (critical, not detected, no longer detected), got %d:\n%s", len(lines), b.String())
	}
	// A survivor in a critical region fails the quality gate, so it is an error. Commas in properties are escaped.
	if want := "::error file=svc/a%2Cb.go,line=3,col=9,title=gamut%3A critical survivor%2C not covered (CONDITIONALS_BOUNDARY)::"; !strings.HasPrefix(lines[0], want) {
		t.Errorf("critical annotation:\n%s\nwant prefix\n%s", lines[0], want)
	}
	if want := "::warning file=svc/a%2Cb.go,line=4,col=9,title=gamut%3A not detected (CONDITIONALS_BOUNDARY)::"; !strings.HasPrefix(lines[1], want) {
		t.Errorf("survivor annotation:\n%s\nwant prefix\n%s", lines[1], want)
	}
	if !strings.Contains(lines[2], "title=gamut%3A no longer detected") || !strings.Contains(lines[2], "TestOld") {
		t.Errorf("no longer detected annotation: %s", lines[2])
	}
	// Newlines in the message become %0A, and the accept command is attached.
	for _, l := range lines {
		msg := l[strings.Index(l, "::")+2:]
		msg = msg[strings.Index(msg, "::")+2:]
		if strings.ContainsAny(msg, "\n\r") || !strings.Contains(msg, "%0A") || !strings.Contains(msg, "gamut accept -reason") {
			t.Errorf("message %q: want escaped newlines and the accept command", msg)
		}
	}
	if !strings.Contains(lines[1], "`<` -> `<=`") {
		t.Errorf("want the mutation in the message: %s", lines[1])
	}
}
