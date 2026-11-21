package report

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

// removals are the mutators that remove code. Their survivors mean either missing tests or unneeded code.
var removals = []string{string(mutant.StatementRemove), string(mutant.ConditionsConstant)}

// WriteSimplify writes the survivors of code-removing mutations, per enclosing function, as candidates for unneeded code.
// Accepted mutants and mutants stopped by disable comments are left out. It does not affect the score or the quality gate.
func WriteSimplify(w io.Writer, r *Report) {
	type group struct {
		file, scope string
		lines       []string
	}
	var groups []*group
	total, uncovered, uninfected := 0, 0, 0
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			if !m.survivor() || !slices.Contains(removals, m.Type) {
				continue
			}
			// For code no test reaches, and code reached without changing a value (such as an error branch no input enters),
			// the missing input is usually the problem rather than whether the code can go, so only the count is printed.
			switch {
			case m.Status == string(run.NotCovered):
				uncovered++
				continue
			case m.Reason == string(run.ReasonNotInfected):
				uninfected++
				continue
			}
			// The scope of an identifier (file::TYPE::scope::hash) names the enclosing function, in the form T.M for a method.
			scope := ""
			if parts := strings.Split(m.ID, "::"); len(parts) >= 4 {
				scope = parts[len(parts)-2]
			}
			var g *group
			if i := slices.IndexFunc(groups, func(g *group) bool { return g.file == f.Path && g.scope == scope }); i >= 0 {
				g = groups[i]
			} else {
				g = &group{file: f.Path, scope: scope}
				groups = append(groups, g)
			}
			// Print only the first line of a multi-line statement.
			original, rest, multiline := strings.Cut(strings.TrimSpace(m.Original), "\n")
			if multiline && rest != "" {
				original = strings.TrimSpace(original) + " …"
			}
			change := fmt.Sprintf("%#q -> %#q", original, m.Replacement)
			if m.Type == string(mutant.StatementRemove) {
				change = fmt.Sprintf("%#q removed", original)
			}
			g.lines = append(g.lines, fmt.Sprintf("%d:%d %s %s  [%s]", m.Line, m.Column, m.Type, change, m.ID))
			total++
		}
	}
	if total == 0 {
		fmt.Fprintln(w, "simplification candidates: none (no surviving STATEMENT_REMOVE or CONDITIONS_CONSTANT mutant that tests reach and change)")
	} else {
		fmt.Fprintf(w, "simplification candidates (%d in %d functions): tests ran this code and the removal changed it, yet no test failed; delete it, or add a test that needs it\n", total, len(groups))
		for _, g := range groups {
			fmt.Fprintf(w, "\n%s: %s\n", g.file, g.scope)
			for _, line := range g.lines {
				fmt.Fprintf(w, "  %s\n", line)
			}
		}
	}
	if uninfected > 0 {
		fmt.Fprintf(w, "\n%d more removals survive because no test input makes them differ (such as an error branch no test enters); that points to a missing input, so they are listed as not infected in the summary, not here\n", uninfected)
	}
	if uncovered > 0 {
		fmt.Fprintf(w, "\n%d more removals survive in code no test reaches; they are listed as not covered in the summary, not here\n", uncovered)
	}
}
