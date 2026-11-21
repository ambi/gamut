package report

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/run"
)

// KillMatrix は、-kill-matrix で、変異体ごとに検出したテストのすべてから求めた、テストの見直しの候補である。
// 包含は今の変異体に対するもので、将来の故障に対するものではないので、テストを消す根拠にはならない。
type KillMatrix struct {
	// Mutants は計算に使った変異体の数、Excluded は、テストが到達したのに検出したテストのすべてが分からないので除いた数
	// （TIMED OUT、PENDING、kill matrix でない判定の KILLED）である。
	Mutants  int `json:"mutants"`
	Excluded int `json:"excluded"`
	// NoKill は、変異体に到達したのに 1 体も検出しなかったテストである。
	NoKill []string `json:"tests_detecting_nothing"`
	// Subsumed は、検出した変異体の集合が、ほかのテストの集合に真に含まれるテストである。
	Subsumed []Subsumption `json:"subsumed_tests"`
	// Duplicates は、検出した変異体の集合が同じテストの組である。
	Duplicates [][]string `json:"duplicate_tests"`
}

// Subsumption は、Test が検出する変異体を、By のどのテストもすべて検出することを表す。
type Subsumption struct {
	Test string   `json:"test"`
	By   []string `json:"by"`
}

// ComputeKillMatrix は、報告の変異体の Killers から KillMatrix を求めて入れる。
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

// writeKillMatrix は、要約に kill matrix の節を書く。
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
