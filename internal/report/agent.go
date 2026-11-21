package report

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/run"
)

// agentTests caps the number of reaching tests listed per survivor.
const agentTests = 10

// AgentReport is the report for coding agents filling survivors.
// Per survivor it has the class, the line before and after the mutation, the source of the enclosing declaration, the reaching tests, and a hint for fixing it.
type AgentReport struct {
	Package   string          `json:"package"`
	Summary   AgentSummary    `json:"summary"`
	Survivors []AgentSurvivor `json:"survivors"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// AgentSummary are the counts and the score.
type AgentSummary struct {
	Killed        int     `json:"killed"`
	Lived         int     `json:"lived"`
	NotCovered    int     `json:"not_covered"`
	Accepted      int     `json:"accepted,omitempty"`
	Pending       int     `json:"pending,omitempty"`
	MutationScore float64 `json:"mutation_score"`
}

// AgentSurvivor is one survivor to fix.
type AgentSurvivor struct {
	ID string `json:"id"`
	// Class is one of not detected, not infected, not covered, and no longer detected.
	Class   string    `json:"class"`
	File    string    `json:"file"`
	Line    int       `json:"line"`
	Column  int       `json:"column"`
	Mutator string    `json:"mutator"`
	Diff    AgentDiff `json:"diff"`
	// Declaration is the source of the top-level declaration enclosing the mutation site, Doc comment included.
	Declaration string `json:"declaration"`
	// Tests are the tests that reached the mutation site, up to 10, those containing the declaration's name first. TestsTotal is their total.
	Tests      []string `json:"tests"`
	TestsTotal int      `json:"tests_total"`
	Hint       string   `json:"hint"`
	Critical   bool     `json:"critical,omitempty"`
	// PreviouslyKilledBy is the test that detected this mutant last time and is now gone.
	PreviouslyKilledBy string `json:"previously_killed_by,omitempty"`
}

// AgentDiff is the line of the mutation site before and after the mutation.
type AgentDiff struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// Agent turns the report into the shape for agents, reading sources and test files from moduleDir.
// The survivors to fix are the unaccepted LIVED and NOT COVERED.
func (r *Report) Agent(moduleDir string) (*AgentReport, error) {
	out := &AgentReport{Package: r.Package, Survivors: []AgentSurvivor{}, Summary: AgentSummary{
		Killed: r.MutantsKilled + r.MutantsTimedOut, Lived: r.MutantsLived, NotCovered: r.MutantsNotCovered,
		Accepted: r.MutantsAccepted, Pending: r.MutantsPending, MutationScore: r.MutationScore,
	}}
	sources := map[string]*sourceFile{}
	dirs := map[string]bool{}
	for _, f := range r.Files {
		dirs[path.Dir(f.Path)] = true
		for _, m := range f.Mutations {
			if !m.survivor() {
				continue
			}
			src := sources[f.Path]
			if src == nil {
				var err error
				if src, err = readSource(filepath.Join(moduleDir, filepath.FromSlash(f.Path))); err != nil {
					return nil, err
				}
				sources[f.Path] = src
			}
			scope := scopeOf(m.ID)
			s := AgentSurvivor{
				ID: m.ID, Class: classOf(m), File: f.Path, Line: m.Line, Column: m.Column, Mutator: m.Type,
				Diff: src.diff(m), Declaration: src.declaration(m.Line), Critical: m.Critical, PreviouslyKilledBy: m.PreviouslyKilledBy,
				Tests: rankTests(m.Tests, scope), TestsTotal: len(m.Tests), Hint: agentHint(m, scope),
			}
			out.Survivors = append(out.Survivors, s)
		}
	}
	for _, d := range slices.Sorted(maps.Keys(dirs)) {
		out.Warnings = append(out.Warnings, testWarnings(moduleDir, d)...)
	}
	for _, name := range r.UnrepeatableTests {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%s fails when run twice in one process (go test -count=2), so it leaves state behind; reset that state with t.Cleanup so the test is independent of earlier runs", name))
	}
	return out, nil
}

// sourceFile is a source read and its syntax tree.
type sourceFile struct {
	src   []byte
	fset  *token.FileSet
	file  *ast.File
	lines []string
}

func readSource(p string) (*sourceFile, error) {
	src, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read the source for the agent report: %w", err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	return &sourceFile{src: src, fset: fset, file: f, lines: strings.Split(string(src), "\n")}, nil
}

// diff returns the line of the mutation site before and after the mutation. Columns are in bytes.
func (s *sourceFile) diff(m Mutation) AgentDiff {
	if m.Line < 1 || m.Line > len(s.lines) {
		return AgentDiff{}
	}
	before := s.lines[m.Line-1]
	at := m.Column - 1
	original := strings.TrimSpace(m.Original)
	if at < 0 || at+len(original) > len(before) || before[at:at+len(original)] != original {
		return AgentDiff{Before: before}
	}
	return AgentDiff{Before: before, After: before[:at] + m.Replacement + before[at+len(original):]}
}

// declaration returns the source of the top-level declaration containing line, Doc comment included.
func (s *sourceFile) declaration(line int) string {
	for _, d := range s.file.Decls {
		start, end := d.Pos(), d.End()
		if s.fset.Position(start).Line > line || s.fset.Position(end).Line < line {
			continue
		}
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
		case *ast.GenDecl:
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
		}
		return string(s.src[s.fset.Position(start).Offset:s.fset.Position(end).Offset])
	}
	return ""
}

// scopeOf returns the name of the enclosing declaration from an identifier (file::TYPE::scope::hash); M for a method T.M.
func scopeOf(id string) string {
	parts := strings.Split(id, "::")
	if len(parts) < 4 {
		return ""
	}
	scope := parts[len(parts)-2]
	return scope[strings.LastIndex(scope, ".")+1:]
}

// rankTests returns up to agentTests tests, those whose name contains scope first.
func rankTests(tests []string, scope string) []string {
	out := slices.Clone(tests)
	slices.SortStableFunc(out, func(a, b string) int {
		ma, mb := scope != "" && strings.Contains(a, scope), scope != "" && strings.Contains(b, scope)
		switch {
		case ma && !mb:
			return -1
		case mb && !ma:
			return 1
		}
		return 0
	})
	if out == nil {
		out = []string{}
	}
	return out[:min(len(out), agentTests)]
}

// agentHint returns a hint for fixing, in line with how Go tests are written.
func agentHint(m Mutation, scope string) string {
	change := fmt.Sprintf("%#q -> %#q", strings.TrimSpace(m.Original), m.Replacement)
	switch {
	case m.PreviouslyKilledBy != "":
		return fmt.Sprintf("%s killed this mutant before and is gone. Restore it, or add a test that fails on %s.", m.PreviouslyKilledBy, change)
	case m.Status == string(run.NotCovered):
		return fmt.Sprintf("No test reaches this code in %s. Add a test that calls it, for example a new case in a table-driven test run with t.Run, and assert the result.", scope)
	case m.Reason == run.ReasonNotInfected:
		switch m.Type {
		case "CONDITIONALS_BOUNDARY":
			return fmt.Sprintf("Tests reach this comparison but never with equal operands, so %s gives the same result. Add a case where both sides are equal and assert the outcome.", change)
		case "ARITHMETIC_BASE":
			return fmt.Sprintf("Tests reach this expression only with operands where %s gives the same value (such as 0 or 1). Add a case with other operands and assert the result.", change)
		}
		return fmt.Sprintf("Tests reach this code but never with a value that %s changes. Add an input that makes the original and the mutated expression differ, and assert the result.", change)
	}
	return fmt.Sprintf("Tests run this code and the value changes with %s, but no assertion checks it. Assert the value this expression produces, for example by comparing the whole result in a table-driven case.", change)
}

// testWarnings finds, in the test files of the module-relative directory dir, tests that may mislead verdicts:
//   - Tests that may call t.Skip: when skipped, mutants only they detect look like survivors.
//   - Tests that start the test binary itself as a child process: reach and failures inside the child may not show in the records and verdicts.
func testWarnings(moduleDir, dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(moduleDir, filepath.FromSlash(dir), "*_test.go"))
	var out []string
	for _, p := range matches {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		rel := path.Join(dir, filepath.Base(p))
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Recv != nil || !strings.HasPrefix(fd.Name.Name, "Test") {
				continue
			}
			var skips, spawns, self bool
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.SelectorExpr:
					switch {
					case n.Sel.Name == "Skip" || n.Sel.Name == "Skipf" || n.Sel.Name == "SkipNow":
						skips = true
					case isIdent(n.X, "exec") && strings.HasPrefix(n.Sel.Name, "Command"):
						spawns = true
					case isIdent(n.X, "os") && n.Sel.Name == "Executable":
						self = true
					}
				case *ast.IndexExpr:
					if sel, ok := n.X.(*ast.SelectorExpr); ok && isIdent(sel.X, "os") && sel.Sel.Name == "Args" {
						self = true
					}
				}
				return true
			})
			line := fset.Position(fd.Pos()).Line
			if skips {
				out = append(out, fmt.Sprintf("%s:%d: %s can skip itself (t.Skip); mutants only it would kill look undetected when it skips", rel, line, fd.Name.Name))
			}
			if spawns && self {
				out = append(out, fmt.Sprintf("%s:%d: %s runs the test binary in a child process; what the child reaches or fails may not appear in the verdicts", rel, line, fd.Name.Name))
			}
		}
	}
	return out
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}
