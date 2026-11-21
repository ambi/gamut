package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestUnrepeatable_TestsAreJudgedInFreshProcesses は、同じプロセスで 2 回目に失敗するテストを見つけて警告し、
// それが到達する変異体を使い回さないプロセスで判定することを確かめる。判定は -isolate と同じになり、実行ごとに揺れない。
// 被覆を測る実行、被覆の記録をキャッシュから戻す実行、キャッシュなしの実行のどれでも同じである。
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
			// 中身を変えずに更新時刻だけを変え、変更のない再実行の近道を外す。
			later := time.Now().Add(time.Minute)
			if err := os.Chtimes(filepath.Join(dir, "unrepeatable_test.go"), later, later); err != nil {
				t.Fatal(err)
			}
			// 判定の記録だけを消し、被覆の記録は戻るが変異体は実行し直すようにする。
			dropVerdicts(t, cachePath)
		}
		reportPath := filepath.Join(t.TempDir(), "r.json")
		var stdout, stderr strings.Builder
		args := append(append([]string{"-workers", "1", "-o", reportPath}, run.args...), dir)
		if code := realMain(args, &stdout, &stderr); code != 0 {
			t.Fatalf("%s: exit %d:\n%s", run.name, code, stderr.String())
		}
		data, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatal(err)
		}
		var rep struct {
			Unrepeatable []string `json:"unrepeatable_tests"`
			Files        []struct {
				Mutations []mutation `json:"mutations"`
			} `json:"files"`
		}
		if err := json.Unmarshal(data, &rep); err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, f := range rep.Files {
			for _, m := range f.Mutations {
				got[m.ID] = m.Status
			}
		}
		for id, w := range want {
			if got[id] != w {
				t.Errorf("%s: %s is %s, -isolate says %s", run.name, id, got[id], w)
			}
		}
		if len(rep.Unrepeatable) != 1 || rep.Unrepeatable[0] != "TestSwitch" {
			t.Errorf("%s: unrepeatable_tests %v, want [TestSwitch]", run.name, rep.Unrepeatable)
		}
		if !strings.Contains(stdout.String(), "TestSwitch") {
			t.Errorf("%s: the summary does not warn about TestSwitch:\n%s", run.name, stdout.String())
		}
		if run.name == "restoring coverage" && !strings.Contains(stderr.String(), "2 restored from the cache") {
			t.Errorf("coverage was not restored:\n%s", stderr.String())
		}
	}
}

// dropVerdicts は、キャッシュファイルから判定の記録を消す。被覆の記録は残す。
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
