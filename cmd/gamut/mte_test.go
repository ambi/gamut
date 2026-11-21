package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReporter_WritesMutationTestingElements は、-o と -reporter json が mutation-testing-elements の形式を書き、
// -reporter html がそれを埋め込んだ HTML を -report-dir に書くことを確かめる。
func TestReporter_WritesMutationTestingElements(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr strings.Builder
	args := []string{"-q", "-no-cache", "-workers", "2", "-reporter", "json", "-reporter", "html", "-report-dir", dir, "-o", output, "../../testdata/fixture"}
	if code := realMain(args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("text was not requested, but the summary was written:\n%s", stdout.String())
	}
	// -o があれば json はそこへ書き、-report-dir には書かない。
	if _, err := os.Stat(filepath.Join(dir, "mutation.json")); err == nil {
		t.Errorf("-reporter json with -o also wrote %s", filepath.Join(dir, "mutation.json"))
	}
	rep := readMTE(t, output)
	if rep.SchemaVersion != "2" || rep.Framework.Name != "gamut" || rep.Framework.Version == "" {
		t.Errorf("schemaVersion %q, framework %+v", rep.SchemaVersion, rep.Framework)
	}
	statuses := map[string]int{}
	for path, f := range rep.Files {
		if f.Source == "" {
			t.Errorf("%s: no source", path)
		}
		for _, m := range f.Mutants {
			statuses[m.Status]++
		}
	}
	if statuses["Killed"] != rep.Gamut.Counts.Killed || statuses["Survived"] != rep.Gamut.Counts.Survived || statuses["Killed"] == 0 {
		t.Errorf("statuses %v, counts %+v", statuses, rep.Gamut.Counts)
	}
	html, err := os.ReadFile(filepath.Join(dir, "mutation.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "<mutation-test-report-app") || !strings.Contains(string(html), `"schemaVersion":"2"`) {
		t.Errorf("the HTML does not embed the report:\n%.400s", html)
	}

	// -o がなければ、json は -report-dir に書く。
	if code := realMain([]string{"-q", "-no-cache", "-workers", "2", "-reporter", "json", "-report-dir", dir, "../../testdata/fixture"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("-reporter json without -o: exit %d", code)
	}
	if readMTE(t, filepath.Join(dir, "mutation.json")).SchemaVersion != "2" {
		t.Error("-reporter json without -o did not write mutation.json")
	}

	for _, kind := range []string{"xml", "gomutants"} {
		if code := realMain([]string{"-reporter", kind, "../../testdata/fixture"}, io.Discard, io.Discard); code != exitError {
			t.Errorf("-reporter %s: exit %d, want %d", kind, code, exitError)
		}
	}
}
