package report

import (
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/ambi/gamut/internal/run"
)

// WriteGitHubAnnotations writes the survivors to fix (unaccepted LIVED and NOT COVERED) as GitHub Actions workflow commands.
// On a pull request they show as annotations on their lines. Survivors in critical regions fail the quality gate, so they are errors; the rest are warnings.
// prefix is the relative path from the repository root to the module root, since annotation files are given relative to the repository.
func WriteGitHubAnnotations(w io.Writer, r *Report, prefix string) {
	for _, f := range r.Files {
		file := path.Join(prefix, f.Path)
		for _, m := range f.Mutations {
			if !m.survivor() {
				continue
			}
			level, class := "warning", classOf(m)
			if m.Critical {
				level, class = "error", "critical survivor, "+class
			}
			title := fmt.Sprintf("gamut: %s (%s)", class, m.Type)
			msg := fmt.Sprintf("%#q -> %#q survives: %s", strings.TrimSpace(m.Original), m.Replacement, hintOf(m))
			if m.PreviouslyKilledBy != "" {
				msg += fmt.Sprintf("\nIt was killed by %s, which is gone.", m.PreviouslyKilledBy)
			}
			msg += fmt.Sprintf("\nIf no input can tell it apart, accept it:\ngamut accept -reason '<why it is equivalent>' '%s'", m.ID)
			fmt.Fprintf(w, "::%s file=%s,line=%d,col=%d,title=%s::%s\n", level, escapeProperty(file), m.Line, m.Column, escapeProperty(title), escapeData(msg))
		}
	}
}

// classOf returns the class of a survivor.
func classOf(m Mutation) string {
	switch {
	case m.PreviouslyKilledBy != "":
		return "no longer detected"
	case m.Status == string(run.NotCovered):
		return "not covered"
	}
	return m.Reason
}

// hintOf returns the hint for fixing a survivor, the same text as for the summary's group.
func hintOf(m Mutation) string {
	switch {
	case m.PreviouslyKilledBy != "":
		return "the tests that killed this mutant last time are gone and no remaining test fails: restore or replace them"
	case m.Status == string(run.NotCovered):
		return "no test reaches the mutant: add a test case"
	case m.Reason == run.ReasonNotInfected:
		return "tests reach the mutant but never with a value it changes: add an input"
	}
	return "tests run the mutant and see a different value, but none fails: strengthen an assertion"
}

// escapeData escapes a workflow command's message by GitHub's rules.
func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// escapeProperty escapes a workflow command's property value by GitHub's rules.
func escapeProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}
