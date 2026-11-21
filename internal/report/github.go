package report

import (
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/ambi/gamut/internal/run"
)

// WriteGitHubAnnotations は、直すべき生き残り（受理していない LIVED と NOT COVERED）を、GitHub Actions のワークフローコマンドで書く。
// Pull Request では、その行に注釈として表示される。重要領域の生き残りは品質の条件を満たさないので error、ほかは warning にする。
// prefix は、リポジトリのルートからモジュールのルートへの相対パスである。注釈のファイルはリポジトリ相対で指定する。
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

// classOf は、生き残りの分類を返す。
func classOf(m Mutation) string {
	switch {
	case m.PreviouslyKilledBy != "":
		return "no longer detected"
	case m.Status == string(run.NotCovered):
		return "not covered"
	}
	return m.Reason
}

// hintOf は、生き残りの直し方の指針を返す。要約の組の指針と同じ文である。
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

// escapeData は、ワークフローコマンドの本文を GitHub の規則でエスケープする。
func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// escapeProperty は、ワークフローコマンドの属性の値を GitHub の規則でエスケープする。
func escapeProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}
