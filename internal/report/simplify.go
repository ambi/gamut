package report

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

// removals は、コードを消す変異演算子である。その生き残りは、テストの不足か、コードが不要かのどちらかである。
var removals = []string{string(mutant.StatementRemove), string(mutant.ConditionsConstant)}

// WriteSimplify は、コードを消す変異の生き残りを、囲む関数ごとに、不要かもしれないコードの候補として書く。
// 受理した変異体と無効化コメントで止めた変異体は含めない。点数と品質の条件には影響しない。
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
			// テストが到達しないコードと、到達しても値が変わらないコード（エラーの分岐に入る入力がないなど）は、
			// 消せるかより入力のないことが問題であることが多いので、数だけを出す。
			switch {
			case m.Status == string(run.NotCovered):
				uncovered++
				continue
			case m.Reason == string(run.ReasonNotInfected):
				uninfected++
				continue
			}
			// 識別子（file::TYPE::scope::hash）の scope は、メソッドなら T.M の形で、囲む関数を表す。
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
			// 複数行の文は、最初の行だけを出す。
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
