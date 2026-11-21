package main

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
)

// mteReport は、テストが読む mutation-testing-elements の報告の部分である。
type mteReport struct {
	SchemaVersion string `json:"schemaVersion"`
	Files         map[string]struct {
		Source  string      `json:"source"`
		Mutants []mteMutant `json:"mutants"`
	} `json:"files"`
	Framework struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"framework"`
	Gamut struct {
		MutationScore float64 `json:"mutationScore"`
		Counts        struct {
			Total    int `json:"total"`
			Killed   int `json:"killed"`
			Survived int `json:"survived"`
			Arid     int `json:"arid"`
			Ignored  int `json:"ignored"`
			Accepted int `json:"accepted"`
		} `json:"counts"`
		StaleAccepted []struct {
			ID  string `json:"id"`
			Why string `json:"why"`
		} `json:"staleAccepted"`
		UnrepeatableTests []string `json:"unrepeatableTests"`
		MaxDepthExcluded  *struct {
			Tests   int `json:"tests"`
			Reaches int `json:"reaches"`
		} `json:"maxDepthExcluded"`
		SkippedPackages []struct {
			Package string `json:"package"`
			Reason  string `json:"reason"`
		} `json:"skippedPackages"`
		KillMatrix *struct {
			TestsDetectingNothing []string `json:"testsDetectingNothing"`
			SubsumedTests         []struct {
				Test string   `json:"test"`
				By   []string `json:"by"`
			} `json:"subsumedTests"`
			DuplicateTests [][]string `json:"duplicateTests"`
		} `json:"killMatrix"`
	} `json:"gamut"`
}

type mteMutant struct {
	ID           string   `json:"id"`
	MutatorName  string   `json:"mutatorName"`
	Status       string   `json:"status"`
	StatusReason string   `json:"statusReason"`
	KilledBy     []string `json:"killedBy"`
	CoveredBy    []string `json:"coveredBy"`
	Gamut        struct {
		Original           string `json:"original"`
		Mode               string `json:"mode"`
		Cached             bool   `json:"cached"`
		Critical           bool   `json:"critical"`
		PreviouslyKilledBy string `json:"previouslyKilledBy"`
		AcceptedStatus     string `json:"acceptedStatus"`
	} `json:"gamut"`
}

// readMTE は、path の mutation-testing-elements の報告を読む。
func readMTE(t *testing.T, path string) *mteReport {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rep mteReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	return &rep
}

// mutants は、報告の変異体を、テストが比べやすい形にして並べる。
func (r *mteReport) mutants() []mutation {
	var out []mutation
	for _, path := range slices.Sorted(maps.Keys(r.Files)) {
		for _, m := range r.Files[path].Mutants {
			mu := mutation{
				ID: m.ID, Type: m.MutatorName, Status: m.Status, Reason: m.StatusReason, Original: m.Gamut.Original, Mode: m.Gamut.Mode, Cached: m.Gamut.Cached,
				Killers: m.KilledBy, PreviouslyKilledBy: m.Gamut.PreviouslyKilledBy, Tests: m.CoveredBy, Critical: m.Gamut.Critical,
				AcceptedStatus: m.Gamut.AcceptedStatus,
			}
			if len(m.KilledBy) > 0 {
				mu.KilledBy = m.KilledBy[0]
			}
			if reason, ok := strings.CutPrefix(m.StatusReason, "accepted: "); ok && m.Status == "Ignored" {
				mu.Accepted = reason
			}
			out = append(out, mu)
		}
	}
	return out
}

// readReport は、path の報告の変異体を読む。
func readReport(t *testing.T, path string) []mutation {
	t.Helper()
	return readMTE(t, path).mutants()
}
