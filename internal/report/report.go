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
}

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
	ElapsedTime       float64  `json:"elapsed_time"`
	Files             []File   `json:"files"`
}

// Build は判定から報告を作る。label は package フィールドに入れる名前、packages は判定したパッケージの import パスである。
func Build(module, label string, packages []string, results []*run.Result, elapsed time.Duration) *Report {
	r := &Report{GoModule: module, Package: label, Packages: packages, ElapsedTime: elapsed.Seconds(), MutantsTotal: len(results)}
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
		})
		switch res.Status {
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
	slices.Sort(order)
	for _, rel := range order {
		r.Files = append(r.Files, *byFile[rel])
	}
	detected := r.MutantsKilled + r.MutantsTimedOut
	if covered := detected + r.MutantsLived; covered > 0 {
		r.TestEfficacy = 100 * float64(detected) / float64(covered)
		r.MutationsCoverage = 100 * float64(covered) / float64(covered+r.MutantsNotCovered)
	}
	return r
}

// WriteJSON は報告を JSON で書く。
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteSummary は、生き残りを理由ごとに並べた要約を書く。
func WriteSummary(w io.Writer, r *Report, results []*run.Result) {
	groups := []struct {
		title, hint string
		match       func(*run.Result) bool
	}{
		{"not detected", "tests run the mutant and see a different value, but none fails: strengthen an assertion",
			func(x *run.Result) bool { return x.Status == run.Lived && x.Reason == run.ReasonNotDetected }},
		{"not infected", "tests reach the mutant but never with a value it changes: add an input",
			func(x *run.Result) bool { return x.Status == run.Lived && x.Reason == run.ReasonNotInfected }},
		{"not covered", "no test reaches the mutant: add a test case",
			func(x *run.Result) bool { return x.Status == run.NotCovered }},
	}
	for _, g := range groups {
		var hits []*run.Result
		for _, x := range results {
			if g.match(x) {
				hits = append(hits, x)
			}
		}
		if len(hits) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s (%d): %s\n", g.title, len(hits), g.hint)
		for _, x := range hits {
			fmt.Fprintf(w, "  %s\n", describe(x.Mutant))
		}
	}
	reused := ""
	if n := countCached(results); n > 0 {
		reused = fmt.Sprintf(", %d reused from the cache", n)
	}
	fmt.Fprintf(w, "\nkilled %d, lived %d, not covered %d, not viable %d, timed out %d (efficacy %.2f%%) in %.1fs%s\n",
		r.MutantsKilled, r.MutantsLived, r.MutantsNotCovered, r.MutantsNotViable, r.MutantsTimedOut, r.TestEfficacy, r.ElapsedTime, reused)
}

func countCached(results []*run.Result) int {
	n := 0
	for _, x := range results {
		if x.Cached {
			n++
		}
	}
	return n
}

func describe(m *mutant.Mutant) string {
	return fmt.Sprintf("%s:%d:%d %s %s -> %s  [%s]", m.File.Rel, m.Line, m.Column, m.Type,
		strings.TrimSpace(m.Original.String()), m.Replacement, m.ID)
}
