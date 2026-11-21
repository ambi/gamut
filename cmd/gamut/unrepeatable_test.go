package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestUnrepeatable_TestsAreJudgedInFreshProcesses checks that tests failing on their second run in the same process are found and warned about,
// and that mutants they reach are judged in processes that are not reused. Verdicts then match -isolate and do not vary between runs.
// This holds for a run that measures coverage, one that restores coverage records from the cache, and one without a cache.
func TestUnrepeatable_TestsAreJudgedInFreshProcesses(t *testing.T) {
	dir := copyModule(t, "../../testdata/unrepeatable")
	isolated, _ := mustGamut(t, "-no-cache", "-isolate", dir)
	want := statuses(isolated)
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	for _, run := range []struct {
		name string
		args []string
	}{
		{"measuring coverage", []string{"-cache", cachePath}},
		{"restoring coverage", []string{"-cache", cachePath}},
		{"without a cache", []string{"-no-cache"}},
	} {
		if run.name == "restoring coverage" {
			// Change only the modification time to skip the unchanged-rerun shortcut.
			later := time.Now().Add(time.Minute)
			if err := os.Chtimes(filepath.Join(dir, "unrepeatable_test.go"), later, later); err != nil {
				t.Fatal(err)
			}
			// Remove only the verdict records, so coverage records are restored but mutants run again.
			dropVerdicts(t, cachePath)
		}
		reportPath := filepath.Join(t.TempDir(), "r.json")
		var stdout, stderr strings.Builder
		args := append(append([]string{"-workers", "1", "-o", reportPath}, run.args...), dir)
		if code := realMain(args, &stdout, &stderr); code != 0 {
			t.Fatalf("%s: exit %d:\n%s", run.name, code, stderr.String())
		}
		rep := readMTE(t, reportPath)
		got := map[string]string{}
		for _, m := range rep.mutants() {
			got[m.ID] = m.Status
		}
		for id, w := range want {
			if got[id] != w {
				t.Errorf("%s: %s is %s, -isolate says %s", run.name, id, got[id], w)
			}
		}
		if u := rep.Gamut.UnrepeatableTests; len(u) != 1 || u[0] != "TestSwitch" {
			t.Errorf("%s: unrepeatable tests %v, want [TestSwitch]", run.name, u)
		}
		if !strings.Contains(stdout.String(), "TestSwitch") {
			t.Errorf("%s: the summary does not warn about TestSwitch:\n%s", run.name, stdout.String())
		}
		if run.name == "restoring coverage" && !strings.Contains(stderr.String(), "2 restored from the cache") {
			t.Errorf("coverage was not restored:\n%s", stderr.String())
		}
	}
}

// dropVerdicts removes the verdict records from the cache file, keeping coverage records.
func dropVerdicts(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f map[string]json.RawMessage
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	delete(f, "mutants")
	if data, err = json.Marshal(f); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
