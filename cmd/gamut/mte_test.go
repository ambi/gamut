package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReporter_WritesMutationTestingElements は、-reporter json と html が、gomutants の形式と同じ変異体を
// mutation-testing-elements の形式で -report-dir に書くことを確かめる。
func TestReporter_WritesMutationTestingElements(t *testing.T) {
	dir := t.TempDir()
	gomutants := filepath.Join(t.TempDir(), "gomutants.json")
	var stdout, stderr strings.Builder
	args := []string{"-q", "-no-cache", "-workers", "2", "-reporter", "json", "-reporter", "html", "-report-dir", dir, "-o", gomutants, "../../testdata/fixture"}
	if code := realMain(args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("text was not requested, but the summary was written:\n%s", stdout.String())
	}
	var mte struct {
		SchemaVersion string `json:"schemaVersion"`
		Files         map[string]struct {
			Source  string `json:"source"`
			Mutants []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"mutants"`
		} `json:"files"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "mutation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &mte); err != nil {
		t.Fatal(err)
	}
	var gm struct {
		Files []struct {
			Path      string     `json:"path"`
			Mutations []mutation `json:"mutations"`
		} `json:"files"`
	}
	data, err = os.ReadFile(gomutants)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &gm); err != nil {
		t.Fatal(err)
	}
	statusOf := map[string]string{"KILLED": "Killed", "LIVED": "Survived", "NOT COVERED": "NoCoverage", "NOT VIABLE": "CompileError", "TIMED OUT": "Timeout"}
	n := 0
	for _, f := range gm.Files {
		got := map[string]string{}
		for _, m := range mte.Files[f.Path].Mutants {
			got[m.ID] = m.Status
		}
		if mte.Files[f.Path].Source == "" {
			t.Errorf("%s: no source", f.Path)
		}
		for _, m := range f.Mutations {
			n++
			if got[m.ID] != statusOf[m.Status] {
				t.Errorf("%s: %s in gomutants, %q in mutation-testing-elements", m.ID, m.Status, got[m.ID])
			}
		}
	}
	if mte.SchemaVersion != "2" || n == 0 {
		t.Errorf("schemaVersion %q with %d mutants", mte.SchemaVersion, n)
	}
	html, err := os.ReadFile(filepath.Join(dir, "mutation.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "<mutation-test-report-app") || !strings.Contains(string(html), `"schemaVersion":"2"`) {
		t.Errorf("the HTML does not embed the report:\n%.400s", html)
	}

	if code := realMain([]string{"-reporter", "xml", "../../testdata/fixture"}, io.Discard, io.Discard); code != exitError {
		t.Errorf("-reporter xml: exit %d, want %d", code, exitError)
	}
}
