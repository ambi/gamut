package report

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/run"
)

// MTEReport is a report in version 2 of the mutation-testing-elements schema (mutation-testing-report-schema). It is gamut's only machine-readable report.
// HTML viewers and dashboards that read this schema can show gamut's verdicts.
// Information particular to gamut goes in the extra field gamut, which the schema allows, so future schema fields cannot collide with it.
// It is the external shape, kept apart from the internal Report and mapped here.
type MTEReport struct {
	SchemaVersion string                  `json:"schemaVersion"`
	Thresholds    MTEThresholds           `json:"thresholds"`
	ProjectRoot   string                  `json:"projectRoot,omitempty"`
	Files         map[string]*MTEFile     `json:"files"`
	TestFiles     map[string]*MTETestFile `json:"testFiles,omitempty"`
	Framework     *MTEFramework           `json:"framework,omitempty"`
	Gamut         *MTEGamut               `json:"gamut,omitempty"`
}

// MTEGamut is the information particular to gamut about the whole report.
type MTEGamut struct {
	GoModule string `json:"goModule"`
	// Package is the import path for one package, or the patterns given to the run for several.
	Package  string   `json:"package"`
	Packages []string `json:"packages,omitempty"`
	// MutationScore is the percentage of detected mutants among detected mutants plus survivors (Survived, NoCoverage). The thresholds apply to it.
	MutationScore float64 `json:"mutationScore"`
	// TestEfficacy is the percentage of reached mutants that were detected, and MutationsCoverage the percentage of judged mutants that were reached.
	TestEfficacy      float64 `json:"testEfficacy"`
	MutationsCoverage float64 `json:"mutationsCoverage"`
	// ThresholdBreak is the threshold below which the score fails the quality gate. 0 disables it.
	ThresholdBreak float64   `json:"thresholdBreak,omitempty"`
	ElapsedTime    float64   `json:"elapsedTime"`
	Counts         MTECounts `json:"counts"`
	// StaleAccepted are the acceptances no longer needed.
	StaleAccepted []MTEStaleAcceptance `json:"staleAccepted,omitempty"`
	// UnrepeatableTests are the tests that failed on their second run in the same process. Mutants they reach were judged without reusing processes.
	UnrepeatableTests []string `json:"unrepeatableTests,omitempty"`
	// MaxDepthExcluded counts the tests removed from reach because they reached mutation sites only beyond -max-depth, and the reaches (pairs of a test and a site).
	MaxDepthExcluded *MTEMaxDepthExcluded `json:"maxDepthExcluded,omitempty"`
	SkippedPackages  []MTESkippedPackage  `json:"skippedPackages,omitempty"`
	KillMatrix       *MTEKillMatrix       `json:"killMatrix,omitempty"`
}

// MTECounts are the numbers of mutants per status. Total and the score leave out Pending and Ignored (accepted, equivalent, disable comment).
type MTECounts struct {
	Total        int `json:"total"`
	Killed       int `json:"killed"`
	Survived     int `json:"survived"`
	NoCoverage   int `json:"noCoverage"`
	CompileError int `json:"compileError"`
	Timeout      int `json:"timeout"`
	Pending      int `json:"pending,omitempty"`
	// Arid is the number of mutants not created because they are in code tests rarely observe (such as logging).
	Arid       int `json:"arid,omitempty"`
	Ignored    int `json:"ignored,omitempty"`
	Equivalent int `json:"equivalent,omitempty"`
	Accepted   int `json:"accepted,omitempty"`
}

// MTEStaleAcceptance is an acceptance no longer needed. Why is detected (a test now detects it) or missing (the mutant is gone).
type MTEStaleAcceptance struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Why    string `json:"why"`
}

// MTEMaxDepthExcluded counts the tests and reaches removed by -max-depth.
type MTEMaxDepthExcluded struct {
	Tests   int `json:"tests"`
	Reaches int `json:"reaches"`
}

// MTESkippedPackage is a package skipped because it cannot be judged, with the reason.
type MTESkippedPackage struct {
	Package string `json:"package"`
	Reason  string `json:"reason"`
}

// MTEKillMatrix are the candidates for reviewing tests under -kill-matrix.
type MTEKillMatrix struct {
	Mutants               int              `json:"mutants"`
	Excluded              int              `json:"excluded"`
	TestsDetectingNothing []string         `json:"testsDetectingNothing"`
	SubsumedTests         []MTESubsumption `json:"subsumedTests"`
	DuplicateTests        [][]string       `json:"duplicateTests"`
}

// MTESubsumption means some test in By detects every mutant Test detects.
type MTESubsumption struct {
	Test string   `json:"test"`
	By   []string `json:"by"`
}

// MTEMutantGamut is the information particular to gamut about a mutant. Fields without values are omitted.
type MTEMutantGamut struct {
	// Original is the text before replacement.
	Original string `json:"original,omitempty"`
	// Mode is whether the mutant was judged embedded in the schemata (schemata) or built standalone (standalone).
	Mode   string `json:"mode,omitempty"`
	Cached bool   `json:"cached,omitempty"`
	// Critical reports whether the mutant is in a critical region.
	Critical bool `json:"critical,omitempty"`
	// PreviouslyKilledBy is the previous detecting test, when that test is gone, the mutant was rechecked, and this time it was not detected.
	PreviouslyKilledBy string `json:"previouslyKilledBy,omitempty"`
	// AcceptedStatus is the judged status of an accepted mutant (whose status is Ignored).
	AcceptedStatus string `json:"acceptedStatus,omitempty"`
}

// MTEThresholds are the thresholds (integer percentages).
type MTEThresholds struct {
	High int `json:"high"`
	Low  int `json:"low"`
}

// MTEFramework is the tool that made the report.
type MTEFramework struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// MTEFile holds the mutants of one file.
type MTEFile struct {
	Language string      `json:"language"`
	Source   string      `json:"source"`
	Mutants  []MTEMutant `json:"mutants"`
}

// MTETestFile is the list of tests. gamut does not record test files, so every test goes in one unnamed file.
type MTETestFile struct {
	Tests []MTETest `json:"tests"`
}

// MTETest is one test, identified by its name.
type MTETest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MTEMutant is one mutant.
type MTEMutant struct {
	ID           string          `json:"id"`
	MutatorName  string          `json:"mutatorName"`
	Replacement  string          `json:"replacement,omitempty"`
	Location     MTELocation     `json:"location"`
	Status       string          `json:"status"`
	StatusReason string          `json:"statusReason,omitempty"`
	KilledBy     []string        `json:"killedBy,omitempty"`
	CoveredBy    []string        `json:"coveredBy,omitempty"`
	Gamut        *MTEMutantGamut `json:"gamut,omitempty"`
}

// MTELocation is the range of the mutated text. Lines and columns count from 1, and end is exclusive.
type MTELocation struct {
	Start MTEPosition `json:"start"`
	End   MTEPosition `json:"end"`
}

// MTEPosition is a line and a column.
type MTEPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// MTE turns the report into mutation-testing-elements format, reading each file's contents from moduleDir.
//
// Statuses map as KILLED→Killed, LIVED→Survived, NOT COVERED→NoCoverage, NOT VIABLE→CompileError, TIMED OUT→Timeout,
// and PENDING→Pending. Uncounted mutants (stopped by disable comments, accepted, EQUIVALENT) become Ignored, with the reason in statusReason.
func (r *Report) MTE(moduleDir string) (*MTEReport, error) {
	out := &MTEReport{SchemaVersion: "2", ProjectRoot: moduleDir, Files: map[string]*MTEFile{}, Framework: &MTEFramework{Name: "gamut", Version: r.Version}}
	t := Thresholds{High: 80, Low: 60}
	if r.Thresholds != nil {
		t = *r.Thresholds
	}
	out.Thresholds = MTEThresholds{High: int(math.Round(t.High)), Low: int(math.Round(t.Low))}
	out.Gamut = r.mteGamut(t.Break)
	tests := map[string]bool{}
	file := func(rel string) (*MTEFile, error) {
		if f := out.Files[rel]; f != nil {
			return f, nil
		}
		src, err := os.ReadFile(filepath.Join(moduleDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("read the source for the mutation-testing-elements report: %w", err)
		}
		f := &MTEFile{Language: "go", Source: string(src), Mutants: []MTEMutant{}}
		out.Files[rel] = f
		return f, nil
	}
	for _, f := range r.Files {
		mf, err := file(f.Path)
		if err != nil {
			return nil, err
		}
		for _, m := range f.Mutations {
			status, reason := mteStatus(m)
			mm := MTEMutant{
				ID: m.ID, MutatorName: m.Type, Replacement: m.Replacement, Status: status, StatusReason: reason,
				Location: span(m.Line, m.Column, m.Original), CoveredBy: m.Tests,
				Gamut: &MTEMutantGamut{Original: m.Original, Mode: m.Mode, Cached: m.Cached, Critical: m.Critical, PreviouslyKilledBy: m.PreviouslyKilledBy},
			}
			if m.Accepted != "" {
				mm.Gamut.AcceptedStatus, _ = mteStatus(Mutation{Status: m.Status, Reason: m.Reason})
			}
			switch {
			case len(m.Killers) > 0:
				mm.KilledBy = m.Killers
			case m.KilledBy != "":
				mm.KilledBy = []string{m.KilledBy}
			}
			for _, name := range append(slices.Clone(m.Tests), mm.KilledBy...) {
				tests[name] = true
			}
			mf.Mutants = append(mf.Mutants, mm)
		}
	}
	for _, ig := range r.Ignored {
		mf, err := file(ig.Path)
		if err != nil {
			return nil, err
		}
		mf.Mutants = append(mf.Mutants, MTEMutant{
			ID: ig.ID, MutatorName: ig.Type, Replacement: ig.Replacement, Status: "Ignored",
			StatusReason: "ignored by comment: " + ig.Reason, Location: span(ig.Line, ig.Column, ig.Original),
			Gamut: &MTEMutantGamut{Original: ig.Original},
		})
	}
	if len(tests) > 0 {
		tf := &MTETestFile{}
		for _, name := range slices.Sorted(maps.Keys(tests)) {
			tf.Tests = append(tf.Tests, MTETest{ID: name, Name: name})
		}
		out.TestFiles = map[string]*MTETestFile{"": tf}
	}
	return out, nil
}

func mteStatus(m Mutation) (string, string) {
	switch {
	case m.Accepted != "":
		return "Ignored", "accepted: " + m.Accepted
	case m.Status == string(run.Equivalent):
		return "Ignored", "equivalent: " + m.Reason
	}
	switch run.Status(m.Status) {
	case run.Killed:
		return "Killed", ""
	case run.Lived:
		return "Survived", m.Reason
	case run.NotCovered:
		return "NoCoverage", ""
	case run.NotViable:
		return "CompileError", ""
	case run.TimedOut:
		return "Timeout", ""
	case run.Pending:
		return "Pending", ""
	}
	return "RuntimeError", m.Status
}

// span returns the range from line:column to the end of the text token, counting columns in bytes.
// For multi-line text (such as a branch body), the end is the line and column after the last newline. Empty text also gets a one-character range.
func span(line, column int, token string) MTELocation {
	start := MTEPosition{line, column}
	if n := strings.Count(token, "\n"); n > 0 {
		return MTELocation{Start: start, End: MTEPosition{line + n, len(token) - strings.LastIndex(token, "\n")}}
	}
	return MTELocation{Start: start, End: MTEPosition{line, column + max(1, len(token))}}
}

// mteGamut maps the information particular to gamut about the whole report.
func (r *Report) mteGamut(thresholdBreak float64) *MTEGamut {
	g := &MTEGamut{
		GoModule: r.GoModule, Package: r.Package, Packages: r.Packages,
		MutationScore: r.MutationScore, TestEfficacy: r.TestEfficacy, MutationsCoverage: r.MutationsCoverage,
		ThresholdBreak: thresholdBreak, ElapsedTime: r.ElapsedTime, UnrepeatableTests: r.UnrepeatableTests,
		Counts: MTECounts{
			Total: r.MutantsTotal, Killed: r.MutantsKilled, Survived: r.MutantsLived, NoCoverage: r.MutantsNotCovered,
			CompileError: r.MutantsNotViable, Timeout: r.MutantsTimedOut, Pending: r.MutantsPending, Arid: r.MutantsArid,
			Ignored: r.MutantsIgnored, Equivalent: r.MutantsEquivalent, Accepted: r.MutantsAccepted,
		},
	}
	for _, s := range r.StaleAccepted {
		g.StaleAccepted = append(g.StaleAccepted, MTEStaleAcceptance(s))
	}
	if r.MaxDepthExcludedTests > 0 {
		g.MaxDepthExcluded = &MTEMaxDepthExcluded{Tests: r.MaxDepthExcludedTests, Reaches: r.MaxDepthExcludedReaches}
	}
	for _, s := range r.SkippedPackages {
		g.SkippedPackages = append(g.SkippedPackages, MTESkippedPackage(s))
	}
	if k := r.KillMatrix; k != nil {
		g.KillMatrix = &MTEKillMatrix{Mutants: k.Mutants, Excluded: k.Excluded, TestsDetectingNothing: k.NoKill, DuplicateTests: k.Duplicates}
		for _, s := range k.Subsumed {
			g.KillMatrix.SubsumedTests = append(g.KillMatrix.SubsumedTests, MTESubsumption(s))
		}
	}
	return g
}

// viewerScript is the viewer script the HTML report loads, pinned to the same version as the report schema.
const viewerScript = "https://cdn.jsdelivr.net/npm/mutation-testing-elements@3.9.0/dist/mutation-test-elements.js"

// WriteHTML writes one HTML file embedding the report. The viewer script is not bundled; a pinned version is loaded.
// encoding/json escapes < and >, so "</script>" in embedded source does not close the script element.
func WriteHTML(w io.Writer, out *MTEReport) error {
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>gamut mutation report</title>
<script src="%s"></script>
</head>
<body>
<mutation-test-report-app title-postfix="gamut"></mutation-test-report-app>
<script>
document.querySelector("mutation-test-report-app").report = %s;
</script>
</body>
</html>
`, viewerScript, data)
	return err
}
