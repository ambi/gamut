// Package report は、判定を gomutants 互換の JSON と標準出力の要約にする。
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

// Mutation は JSON の 1 体の変異体である。gomutants のフィールドに gamut のフィールドを足す。
type Mutation struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Status      string   `json:"status"`
	Line        int      `json:"line"`
	Column      int      `json:"column"`
	Original    string   `json:"original"`
	Replacement string   `json:"replacement"`
	Reason      string   `json:"reason,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	Tests       []string `json:"tests,omitempty"`
	KilledBy    string   `json:"killed_by,omitempty"`
	// Cached は、判定を実行せずに前の実行から再利用したかである。gomutants にはないフィールドである。
	Cached bool `json:"cached,omitempty"`
	// PreviouslyKilledBy は、前回の検出テストが消えたので再検査し、今回は検出されなかったときの、前回の検出テストである。
	// gomutants にはないフィールドである。
	PreviouslyKilledBy string `json:"previously_killed_by,omitempty"`
	// Accepted は、等価な変異体として受理したときの理由である。受理した変異体は判定を保つが、数と効率に含めない。
	// gomutants にはないフィールドである。
	Accepted string `json:"accepted,omitempty"`
}

// StaleAcceptance は、もう要らない受理である。gamut accept -prune で消せる。
type StaleAcceptance struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	// Why は、detected（テストが検出するようになった）か missing（変異体がなくなった）である。
	Why string `json:"why"`
}

// 受理が要らなくなった理由である。
const (
	StaleDetected = "detected"
	StaleMissing  = "missing"
)

// File は JSON の 1 ファイルである。
type File struct {
	FileName  string     `json:"file_name"`
	Path      string     `json:"path"`
	Mutations []Mutation `json:"mutations"`
}

// Report は JSON の報告の全体である。
type Report struct {
	GoModule string `json:"go_module"`
	// Package は、1 パッケージならその import パス、複数なら実行時に指定したパターンである。
	Package string `json:"package"`
	// Packages は判定したパッケージの import パスである。gomutants にはないフィールドである。
	Packages          []string `json:"packages,omitempty"`
	TestEfficacy      float64  `json:"test_efficacy"`
	MutationsCoverage float64  `json:"mutations_coverage"`
	MutantsTotal      int      `json:"mutants_total"`
	MutantsKilled     int      `json:"mutants_killed"`
	MutantsLived      int      `json:"mutants_lived"`
	MutantsNotViable  int      `json:"mutants_not_viable"`
	MutantsNotCovered int      `json:"mutants_not_covered"`
	MutantsTimedOut   int      `json:"mutants_timed_out"`
	// MutantsPending は、選択の外で判定しなかった変異体の数である。MutantsTotal と効率の計算には含めない。gomutants にはないフィールドである。
	MutantsPending int `json:"mutants_pending,omitempty"`
	// MutantsIgnored は、無効化コメントで止めた変異体の数である。MutantsTotal と効率の計算には含めない。gomutants にはないフィールドである。
	MutantsIgnored int     `json:"mutants_ignored,omitempty"`
	ElapsedTime    float64 `json:"elapsed_time"`
	Files          []File  `json:"files"`
	// Ignored は、無効化コメントで止めた変異体である。gomutants の状態の名前を保つため、files[].mutations には入れない。
	// gomutants にはないフィールドである。
	Ignored []IgnoredMutation `json:"ignored,omitempty"`
	// MutantsEquivalent は、コンパイル結果が元と同じで EQUIVALENT とした変異体の数である。MutantsTotal と効率の計算には含めない。
	// gomutants にはないフィールドである。
	MutantsEquivalent int `json:"mutants_equivalent,omitempty"`
	// MutantsAccepted は、受理した変異体のうち判定したものの数である。MutantsTotal と効率の計算には含めない。gomutants にはないフィールドである。
	MutantsAccepted int `json:"mutants_accepted,omitempty"`
	// StaleAccepted は、判定したパッケージの受理のうち、もう要らないものである。gomutants にはないフィールドである。
	StaleAccepted []StaleAcceptance `json:"stale_accepted,omitempty"`
}

// IgnoredMutation は、無効化コメントで止めた 1 体の変異体である。
type IgnoredMutation struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Path        string `json:"path"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
	// Reason は、コメントの -- の後ろに書いた理由である。
	Reason string `json:"reason,omitempty"`
}

// AddIgnored は、無効化コメントで止めた変異体を報告に足す。
func (r *Report) AddIgnored(ignored []mutant.Ignored) {
	for _, ig := range ignored {
		m := ig.Mutant
		r.Ignored = append(r.Ignored, IgnoredMutation{
			ID: m.ID, Type: string(m.Type), Path: m.File.Rel, Line: m.Line, Column: m.Column,
			Original: m.Original.String(), Replacement: m.Replacement.String(), Reason: ig.Reason,
		})
	}
	slices.SortFunc(r.Ignored, func(a, b IgnoredMutation) int {
		if a.Path != b.Path {
			return strings.Compare(a.Path, b.Path)
		}
		return a.Line - b.Line
	})
	r.MutantsIgnored = len(r.Ignored)
}

// Build は判定から報告を作る。label は package フィールドに入れる名前、packages は判定したパッケージの import パスである。
func Build(module, label string, packages []string, results []*run.Result, elapsed time.Duration) *Report {
	r := &Report{GoModule: module, Package: label, Packages: packages, ElapsedTime: elapsed.Seconds()}
	byFile := map[string]*File{}
	var order []string
	for _, res := range results {
		m := res.Mutant
		f, ok := byFile[m.File.Rel]
		if !ok {
			f = &File{FileName: path.Base(m.File.Rel), Path: m.File.Rel}
			byFile[m.File.Rel] = f
			order = append(order, m.File.Rel)
		}
		f.Mutations = append(f.Mutations, Mutation{
			ID: m.ID, Type: string(m.Type), Status: string(res.Status),
			Line: m.Line, Column: m.Column,
			Original: m.Original.String(), Replacement: m.Replacement.String(),
			Reason: res.Reason, Mode: res.Mode, Tests: res.Tests, KilledBy: res.KilledBy, Cached: res.Cached,
			PreviouslyKilledBy: res.PreviouslyKilledBy,
		})
	}
	slices.Sort(order)
	for _, rel := range order {
		r.Files = append(r.Files, *byFile[rel])
	}
	r.count()
	return r
}

// count は、変異体の状態の数と効率を数え直す。PENDING と受理した変異体は数えない。
func (r *Report) count() {
	r.MutantsTotal, r.MutantsKilled, r.MutantsLived, r.MutantsNotViable = 0, 0, 0, 0
	r.MutantsNotCovered, r.MutantsTimedOut, r.MutantsPending, r.MutantsAccepted, r.MutantsEquivalent = 0, 0, 0, 0, 0
	r.TestEfficacy, r.MutationsCoverage = 0, 0
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			switch {
			case m.Status == string(run.Pending):
				r.MutantsPending++
				continue
			case m.Status == string(run.Equivalent):
				r.MutantsEquivalent++
				continue
			case m.Accepted != "":
				r.MutantsAccepted++
				continue
			}
			r.MutantsTotal++
			switch run.Status(m.Status) {
			case run.Killed:
				r.MutantsKilled++
			case run.Lived:
				r.MutantsLived++
			case run.NotViable:
				r.MutantsNotViable++
			case run.NotCovered:
				r.MutantsNotCovered++
			case run.TimedOut:
				r.MutantsTimedOut++
			}
		}
	}
	detected := r.MutantsKilled + r.MutantsTimedOut
	if covered := detected + r.MutantsLived; covered > 0 {
		r.TestEfficacy = 100 * float64(detected) / float64(covered)
		r.MutationsCoverage = 100 * float64(covered) / float64(covered+r.MutantsNotCovered)
	}
}

// ApplyAccepted は、受理を報告に当てる。ids は、判定したパッケージのファイルを指す受理の識別子で、reason はその理由を引く。
// 受理した変異体は判定を保ったまま accepted に理由を入れ、数と効率から外す。
// 受理のうち、テストが検出するようになったもの（KILLED、TIMED OUT）と、変異体がなくなったものを StaleAccepted に並べる。
// PENDING の変異体は、今回判定していないので、どちらにもしない。
func (r *Report) ApplyAccepted(ids []string, reason func(id string) string) {
	found := map[string]bool{}
	for i := range r.Files {
		for k := range r.Files[i].Mutations {
			m := &r.Files[i].Mutations[k]
			why := reason(m.ID)
			if why == "" {
				continue
			}
			found[m.ID] = true
			if m.Status == string(run.Pending) {
				continue
			}
			m.Accepted = why
			if m.Status == string(run.Killed) || m.Status == string(run.TimedOut) {
				r.StaleAccepted = append(r.StaleAccepted, StaleAcceptance{ID: m.ID, Reason: why, Why: StaleDetected})
			}
		}
	}
	for _, ig := range r.Ignored {
		found[ig.ID] = true
	}
	for _, id := range ids {
		if !found[id] {
			r.StaleAccepted = append(r.StaleAccepted, StaleAcceptance{ID: id, Reason: reason(id), Why: StaleMissing})
		}
	}
	slices.SortFunc(r.StaleAccepted, func(a, b StaleAcceptance) int { return strings.Compare(a.ID, b.ID) })
	r.count()
}

// WriteJSON は報告を JSON で書く。
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteSummary は、生き残りを理由ごとに並べた要約を書く。報告だけから書くので、保存した報告からも同じ要約を書ける。
func WriteSummary(w io.Writer, r *Report) {
	lived, covered := string(run.Lived), string(run.NotCovered)
	groups := []struct {
		title, hint string
		match       func(Mutation) bool
	}{
		{"no longer detected", "the tests that killed these mutants last time are gone and no remaining test fails: restore or replace them",
			func(x Mutation) bool { return x.PreviouslyKilledBy != "" }},
		{"not detected", "tests run the mutant and see a different value, but none fails: strengthen an assertion",
			func(x Mutation) bool {
				return x.PreviouslyKilledBy == "" && x.Status == lived && x.Reason == run.ReasonNotDetected
			}},
		{"not infected", "tests reach the mutant but never with a value it changes: add an input",
			func(x Mutation) bool {
				return x.PreviouslyKilledBy == "" && x.Status == lived && x.Reason == run.ReasonNotInfected
			}},
		{"not covered", "no test reaches the mutant: add a test case",
			func(x Mutation) bool { return x.PreviouslyKilledBy == "" && x.Status == covered }},
	}
	cached := 0
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			if m.Cached {
				cached++
			}
		}
	}
	for _, g := range groups {
		var lines []string
		for _, f := range r.Files {
			for _, m := range f.Mutations {
				// 受理した生き残りは、直すものとして並べない。
				if m.Accepted != "" || !g.match(m) {
					continue
				}
				line := fmt.Sprintf("%s:%d:%d %s %s -> %s  [%s]", f.Path, m.Line, m.Column, m.Type, strings.TrimSpace(m.Original), m.Replacement, m.ID)
				if m.PreviouslyKilledBy != "" {
					line += "  previously killed by " + m.PreviouslyKilledBy
				}
				lines = append(lines, line)
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s (%d): %s\n", g.title, len(lines), g.hint)
		for _, line := range lines {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	for _, why := range []struct{ kind, title string }{
		{StaleDetected, "accepted but now detected (%d): a test kills these mutants now; remove them with gamut accept -prune"},
		{StaleMissing, "accepted but gone (%d): these mutants no longer exist (deleted, or their declaration was rewritten); remove them with gamut accept -prune, or accept the new IDs"},
	} {
		var stale []StaleAcceptance
		for _, s := range r.StaleAccepted {
			if s.Why == why.kind {
				stale = append(stale, s)
			}
		}
		if len(stale) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n"+why.title+"\n", len(stale))
		for _, s := range stale {
			fmt.Fprintf(w, "  %s  (%s)\n", s.ID, s.Reason)
		}
	}
	reused := ""
	if cached > 0 {
		reused = fmt.Sprintf(", %d reused from the cache", cached)
	}
	if r.MutantsAccepted > 0 {
		reused += fmt.Sprintf(", %d accepted", r.MutantsAccepted)
	}
	if r.MutantsEquivalent > 0 {
		reused += fmt.Sprintf(", %d equivalent by machine code", r.MutantsEquivalent)
	}
	if r.MutantsPending > 0 {
		reused += fmt.Sprintf(", %d pending outside the selection", r.MutantsPending)
	}
	if r.MutantsIgnored > 0 {
		reused += fmt.Sprintf(", %d ignored by comments", r.MutantsIgnored)
	}
	fmt.Fprintf(w, "\nkilled %d, lived %d, not covered %d, not viable %d, timed out %d (efficacy %.2f%%) in %.1fs%s\n",
		r.MutantsKilled, r.MutantsLived, r.MutantsNotCovered, r.MutantsNotViable, r.MutantsTimedOut, r.TestEfficacy, r.ElapsedTime, reused)
}

// MarkReused は、実行して決まる判定（KILLED、TIMED OUT、not detected の LIVED、EQUIVALENT、個別ビルドの NOT VIABLE）を、
// キャッシュから再利用したとする。保存した報告を、変更のない再実行の報告として返すときに使う。
func (r *Report) MarkReused() {
	for i := range r.Files {
		for k := range r.Files[i].Mutations {
			m := &r.Files[i].Mutations[k]
			switch {
			case m.Status == string(run.Killed), m.Status == string(run.TimedOut), m.Status == string(run.Equivalent),
				m.Status == string(run.Lived) && m.Reason == run.ReasonNotDetected,
				m.Status == string(run.NotViable) && m.Mode == run.ModeStandalone:
				m.Cached = true
			}
		}
	}
}
