// Package report turns verdicts into reports (mutation-testing-elements JSON and HTML, JSON for agents, GitHub annotations) and the summary on stdout.
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

// Mutation is the verdict of one mutant.
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
	// Cached reports whether the verdict was reused from a previous run without running.
	Cached bool `json:"cached,omitempty"`
	// PreviouslyKilledBy is the previous detecting test, when that test is gone, the mutant was rechecked, and this time it was not detected.
	PreviouslyKilledBy string `json:"previously_killed_by,omitempty"`
	// Accepted is the reason when the mutant was accepted as equivalent. Accepted mutants keep their verdicts but are left out of counts and efficacy.
	Accepted string `json:"accepted,omitempty"`
	// Critical reports whether the mutant is in a critical region. Survivors in critical regions fail the quality gate.
	Critical bool `json:"critical,omitempty"`
	// Killers are, under -kill-matrix, all the detecting top-level tests.
	Killers []string `json:"killers,omitempty"`
}

// survivor reports whether m is a survivor to fix (LIVED or NOT COVERED, and not accepted).
func (m Mutation) survivor() bool {
	return m.Accepted == "" && (m.Status == string(run.Lived) || m.Status == string(run.NotCovered))
}

// Thresholds are the thresholds of the score (MutationScore): high at or above High, low at or above Low, and danger below.
// When Break is positive, a score below it fails the quality gate.
type Thresholds struct {
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Break float64 `json:"break,omitempty"`
}

// Label returns the label of score by the thresholds (high, low, danger).
func (t Thresholds) Label(score float64) string {
	switch {
	case score >= t.High:
		return "high"
	case score >= t.Low:
		return "low"
	}
	return "danger"
}

// SkippedPackage is a package skipped because it cannot be judged, with the reason.
type SkippedPackage struct {
	Package string `json:"package"`
	Reason  string `json:"reason"`
}

// StaleAcceptance is an acceptance no longer needed. gamut accept -prune removes it.
type StaleAcceptance struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	// Why is detected (a test now detects it) or missing (the mutant is gone).
	Why string `json:"why"`
}

// The reasons an acceptance is no longer needed.
const (
	StaleDetected = "detected"
	StaleMissing  = "missing"
)

// File is one file of the JSON.
type File struct {
	FileName  string     `json:"file_name"`
	Path      string     `json:"path"`
	Mutations []Mutation `json:"mutations"`
}

// Report is all the verdicts of one run. The summary, every report format, and the quality gate come from it.
// It is serialized only as internal state (the cache for replaying unchanged reruns and merging shards), never as an external shape.
type Report struct {
	GoModule string `json:"go_module"`
	// Version is the gamut version that judged.
	Version string `json:"version,omitempty"`
	// ModuleDir is the directory of the judged module. gamut merge reads the original sources embedded in the report from it.
	ModuleDir string `json:"module_dir,omitempty"`
	// Package is the import path for one package, or the patterns given to the run for several.
	Package string `json:"package"`
	// Packages are the import paths of the judged packages.
	Packages          []string `json:"packages,omitempty"`
	TestEfficacy      float64  `json:"test_efficacy"`
	MutationsCoverage float64  `json:"mutations_coverage"`
	// MutationScore is the share of detected mutants (KILLED, TIMED OUT) among detected mutants plus survivors (LIVED, NOT COVERED).
	// Unlike TestEfficacy, it counts mutants no test reaches as undetected. The thresholds apply to this score.
	MutationScore float64 `json:"mutation_score"`
	// Thresholds are the thresholds used for the summary's label.
	Thresholds        *Thresholds `json:"thresholds,omitempty"`
	MutantsTotal      int         `json:"mutants_total"`
	MutantsKilled     int         `json:"mutants_killed"`
	MutantsLived      int         `json:"mutants_lived"`
	MutantsNotViable  int         `json:"mutants_not_viable"`
	MutantsNotCovered int         `json:"mutants_not_covered"`
	MutantsTimedOut   int         `json:"mutants_timed_out"`
	// MutantsPending is the number of mutants outside the selection, not judged. It is left out of MutantsTotal and efficacy.
	MutantsPending int `json:"mutants_pending,omitempty"`
	// MutantsArid is the number of mutants not created because they are in code tests rarely observe (such as logging; arid code).
	MutantsArid int `json:"mutants_arid,omitempty"`
	// UnrepeatableTests are the names of the tests that failed on their second run in the same process. Mutants they reach were judged without reusing processes.
	UnrepeatableTests []string `json:"unrepeatable_tests,omitempty"`
	// MaxDepthExcludedTests is the number of tests removed from a site's reach because they reached it only beyond -max-depth.
	// MaxDepthExcludedReaches is the number of removed reaches (pairs of a test and a mutation site).
	MaxDepthExcludedTests   int `json:"max_depth_excluded_tests,omitempty"`
	MaxDepthExcludedReaches int `json:"max_depth_excluded_reaches,omitempty"`
	// SkippedPackages are the packages that matched the patterns but were skipped because they cannot be judged (such as when cgo preprocessing fails).
	SkippedPackages []SkippedPackage `json:"skipped_packages,omitempty"`
	// KillMatrix are the candidates for reviewing tests under -kill-matrix.
	KillMatrix *KillMatrix `json:"kill_matrix,omitempty"`
	// MutantsIgnored is the number of mutants stopped by disable comments. It is left out of MutantsTotal and efficacy.
	MutantsIgnored int     `json:"mutants_ignored,omitempty"`
	ElapsedTime    float64 `json:"elapsed_time"`
	Files          []File  `json:"files"`
	// Ignored are the mutants stopped by disable comments. They are not judged, so they are kept apart from Files.
	Ignored []IgnoredMutation `json:"ignored,omitempty"`
	// MutantsEquivalent is the number of mutants found EQUIVALENT because they compile to the original's code. It is left out of MutantsTotal and efficacy.
	MutantsEquivalent int `json:"mutants_equivalent,omitempty"`
	// MutantsAccepted is the number of judged accepted mutants. It is left out of MutantsTotal and efficacy.
	MutantsAccepted int `json:"mutants_accepted,omitempty"`
	// StaleAccepted are the acceptances of the judged packages that are no longer needed.
	StaleAccepted []StaleAcceptance `json:"stale_accepted,omitempty"`
}

// IgnoredMutation is one mutant stopped by a disable comment.
type IgnoredMutation struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Path        string `json:"path"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
	// Reason is the reason written after -- in the comment.
	Reason string `json:"reason,omitempty"`
}

// AddIgnored adds the mutants stopped by disable comments to the report.
func (r *Report) AddIgnored(ignored []mutant.Ignored) {
	for _, ig := range ignored {
		m := ig.Mutant
		r.Ignored = append(r.Ignored, IgnoredMutation{
			ID: m.ID, Type: string(m.Type), Path: m.File.Rel, Line: m.Line, Column: m.Column,
			Original: m.OriginalText, Replacement: m.ReplacementText, Reason: ig.Reason,
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

// Build builds the report from verdicts. label is the name for the package field, and packages are the import paths of the judged packages.
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
			Original: m.OriginalText, Replacement: m.ReplacementText,
			Reason: res.Reason, Mode: res.Mode, Tests: res.Tests, KilledBy: res.KilledBy, Cached: res.Cached,
			PreviouslyKilledBy: res.PreviouslyKilledBy, Killers: res.Killers,
		})
	}
	slices.Sort(order)
	for _, rel := range order {
		r.Files = append(r.Files, *byFile[rel])
	}
	r.count()
	return r
}

// count recomputes the counts per status and efficacy. PENDING and accepted mutants are not counted.
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
	// Without mutants to judge, nothing went undetected either, so it is 100.
	r.MutationScore = 100
	if all := detected + r.MutantsLived + r.MutantsNotCovered; all > 0 {
		r.MutationScore = 100 * float64(detected) / float64(all)
	}
}

// MarkCritical marks the mutants whose identifiers are in ids as being in critical regions.
func (r *Report) MarkCritical(ids map[string]bool) {
	for i := range r.Files {
		for k := range r.Files[i].Mutations {
			m := &r.Files[i].Mutations[k]
			m.Critical = ids[m.ID]
		}
	}
}

// Quality returns why the quality gate fails, or empty when it passes:
//   - A critical region has an unaccepted survivor (LIVED, NOT COVERED).
//   - Thresholds.Break is positive and the score is below it.
//   - With failOnSurvived, there is any unaccepted survivor.
//
// PENDING, EQUIVALENT, and accepted mutants do not count as survivors. In a -since run it applies only to the selected mutants.
func (r *Report) Quality(failOnSurvived bool) []string {
	var critical, survivors int
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			if !m.survivor() {
				continue
			}
			survivors++
			if m.Critical {
				critical++
			}
		}
	}
	var out []string
	if critical > 0 {
		out = append(out, fmt.Sprintf("%d survivors in critical regions", critical))
	}
	if t := r.Thresholds; t != nil && t.Break > 0 && r.MutationScore < t.Break {
		out = append(out, fmt.Sprintf("mutation score %.2f%% is below the break threshold %g%%", r.MutationScore, t.Break))
	}
	if failOnSurvived && survivors > 0 {
		out = append(out, fmt.Sprintf("%d survivors with -fail-on-survived", survivors))
	}
	return out
}

// ApplyAccepted applies acceptances to the report. ids are the accepted identifiers pointing at files of the judged packages, and reason looks up their reasons.
// Accepted mutants keep their verdicts, get the reason in accepted, and are left out of counts and efficacy.
// Acceptances that a test now detects (KILLED, TIMED OUT) and those whose mutant is gone are listed in StaleAccepted.
// PENDING mutants were not judged this time, so they are neither.
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

// WriteJSON writes the report as JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteSummary writes the summary listing survivors by reason. It writes only from the report, so a saved report gives the same summary.
func WriteSummary(w io.Writer, r *Report) {
	lived, covered := string(run.Lived), string(run.NotCovered)
	groups := []struct {
		title, hint string
		match       func(Mutation) bool
	}{
		// Survivors in critical regions fail the quality gate, so they come first and appear in no other group.
		{"critical survivors", "every mutant in a critical region must be detected or accepted",
			func(x Mutation) bool { return x.Critical && x.survivor() }},
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
	for i, g := range groups {
		var lines []string
		for _, f := range r.Files {
			for _, m := range f.Mutations {
				// Accepted survivors are not listed as ones to fix. Survivors in critical regions appear only in the first group.
				if m.Accepted != "" || i > 0 && m.Critical && m.survivor() || !g.match(m) {
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
	if r.KillMatrix != nil {
		writeKillMatrix(w, r.KillMatrix)
	}
	if len(r.SkippedPackages) > 0 {
		fmt.Fprintf(w, "\nskipped packages (%d): not judged; their mutants are not counted\n", len(r.SkippedPackages))
		for _, s := range r.SkippedPackages {
			fmt.Fprintf(w, "  %s  (%s)\n", s.Package, s.Reason)
		}
	}
	if len(r.UnrepeatableTests) > 0 {
		fmt.Fprintf(w, "\nunrepeatable tests (%d): these fail when run twice in one process, so mutants they reach ran in fresh processes; reset the state they leave behind (t.Cleanup) to make the run faster\n", len(r.UnrepeatableTests))
		for _, name := range r.UnrepeatableTests {
			fmt.Fprintf(w, "  %s\n", name)
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
	if r.MaxDepthExcludedTests > 0 {
		reused += fmt.Sprintf(", %d tests reaching %d sites only beyond -max-depth not counted", r.MaxDepthExcludedTests, r.MaxDepthExcludedReaches)
	}
	if r.MutantsArid > 0 {
		reused += fmt.Sprintf(", %d skipped in arid code", r.MutantsArid)
	}
	fmt.Fprintf(w, "\nkilled %d, lived %d, not covered %d, not viable %d, timed out %d (efficacy %.2f%%) in %.1fs%s\n",
		r.MutantsKilled, r.MutantsLived, r.MutantsNotCovered, r.MutantsNotViable, r.MutantsTimedOut, r.TestEfficacy, r.ElapsedTime, reused)
	score := fmt.Sprintf("mutation score %.2f%%", r.MutationScore)
	if r.Thresholds != nil {
		score += " [" + r.Thresholds.Label(r.MutationScore) + "]"
	}
	var critical, criticalSurvived int
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			if m.Critical && m.Status != string(run.Pending) {
				critical++
				if m.survivor() {
					criticalSurvived++
				}
			}
		}
	}
	if critical > 0 {
		score += fmt.Sprintf("; critical regions: %d mutants, %d survived", critical, criticalSurvived)
	}
	fmt.Fprintln(w, score)
}

// MarkReused marks the verdicts decided by running (KILLED, TIMED OUT, LIVED not detected, EQUIVALENT, NOT VIABLE of standalone builds)
// as reused from the cache. It is used when returning a saved report as the report of an unchanged rerun.
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
