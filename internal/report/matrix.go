package report

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/run"
)

// KillMatrix are the candidates for reviewing tests, computed under -kill-matrix from all the detecting tests of each mutant.
// Subsumption is relative to the current mutants, not to future faults, so it is no reason to delete tests.
type KillMatrix struct {
	// Mutants is the number of mutants used, and Excluded the number left out because tests reached them but not all detecting tests are known
	// (TIMED OUT, PENDING, KILLED not judged by the kill matrix).
	Mutants  int `json:"mutants"`
	Excluded int `json:"excluded"`
	// NoKill are tests that reached mutants but detected none.
	NoKill []string `json:"tests_detecting_nothing"`
	// Subsumed are tests whose set of detected mutants is a proper subset of another test's.
	Subsumed []Subsumption `json:"subsumed_tests"`
	// Duplicates are groups of tests with the same set of detected mutants.
	Duplicates [][]string `json:"duplicate_tests"`
}

// Subsumption means every test in By detects every mutant Test detects.
type Subsumption struct {
	Test string   `json:"test"`
	By   []string `json:"by"`
}

// ComputeKillMatrix computes the KillMatrix from the Killers of the report's mutants and sets it.
func (r *Report) ComputeKillMatrix() {
	km := &KillMatrix{NoKill: []string{}, Subsumed: []Subsumption{}, Duplicates: [][]string{}}
	kills := map[string]map[string]bool{}
	note := func(test string) {
		if kills[test] == nil {
			kills[test] = map[string]bool{}
		}
	}
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			if len(m.Tests) == 0 {
				continue
			}
			switch {
			case m.Status == string(run.Lived):
			case m.Status == string(run.Killed) && len(m.Killers) > 0:
			case m.Status == string(run.TimedOut), m.Status == string(run.Pending), m.Status == string(run.Killed):
				km.Excluded++
				continue
			default:
				continue
			}
			km.Mutants++
			for _, t := range m.Tests {
				note(t)
			}
			for _, t := range m.Killers {
				note(t)
				kills[t][m.ID] = true
			}
		}
	}
	tests := slices.Sorted(maps.Keys(kills))
	grouped := map[string]bool{}
	for i, a := range tests {
		if len(kills[a]) == 0 {
			km.NoKill = append(km.NoKill, a)
			continue
		}
		var by []string
		for _, b := range tests {
			if a != b && len(kills[b]) > len(kills[a]) && subset(kills[a], kills[b]) {
				by = append(by, b)
			}
		}
		if len(by) > 0 {
			km.Subsumed = append(km.Subsumed, Subsumption{Test: a, By: by})
		}
		if grouped[a] {
			continue
		}
		group := []string{a}
		for _, b := range tests[i+1:] {
			if maps.Equal(kills[a], kills[b]) {
				group = append(group, b)
				grouped[b] = true
			}
		}
		if len(group) > 1 {
			km.Duplicates = append(km.Duplicates, group)
		}
	}
	r.KillMatrix = km
}

func subset(a, b map[string]bool) bool {
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// writeKillMatrix writes the kill matrix section of the summary.
func writeKillMatrix(w io.Writer, km *KillMatrix) {
	fmt.Fprintf(w, "\nkill matrix (%d mutants, %d excluded as timed out or not fully run): candidates to review, not to delete; subsumption holds only for these mutants\n", km.Mutants, km.Excluded)
	if len(km.NoKill) > 0 {
		fmt.Fprintf(w, "  tests detecting nothing (%d): they reach mutants but fail for none; check their assertions\n", len(km.NoKill))
		for _, t := range km.NoKill {
			fmt.Fprintf(w, "    %s\n", t)
		}
	}
	if len(km.Subsumed) > 0 {
		fmt.Fprintf(w, "  subsumed tests (%d): every mutant they detect is also detected by another test\n", len(km.Subsumed))
		for _, s := range km.Subsumed {
			fmt.Fprintf(w, "    %s  (by %s)\n", s.Test, strings.Join(s.By, ", "))
		}
	}
	if len(km.Duplicates) > 0 {
		fmt.Fprintf(w, "  duplicate tests (%d groups): each group detects the same mutants\n", len(km.Duplicates))
		for _, g := range km.Duplicates {
			fmt.Fprintf(w, "    %s\n", strings.Join(g, ", "))
		}
	}
}
