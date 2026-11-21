package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelectTests は、gamut select-tests が、変わった宣言の変異体を検出したテスト、そこに到達したテスト、変わったテストの順に、
// 何も実行せずにテストを選ぶことを確かめる。キャッシュがなければ終了コード 1 で止まる。
func TestSelectTests(t *testing.T) {
	dir := copyModule(t, "../../testdata/selecttests")
	gitRepo(t, dir)
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	var stdout, stderr strings.Builder
	if code := realMain([]string{"-q", "-kill-matrix", "-cache", cachePath, dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("gamut: exit %d:\n%s", code, stderr.String())
	}
	// Add を書き換え、TestMul が使う補助の check も書き換える。Mul は変えない。
	edit(t, filepath.Join(dir, "calc.go"), "return a + b", "return b + a")
	edit(t, filepath.Join(dir, "calc_test.go"), "t.Fatalf(\"got %d, want %d\", got, want)", "t.Fatalf(\"got %d; want %d\", got, want)")

	selectTests := func(format string) (string, string, int) {
		var stdout, stderr strings.Builder
		code := realMain([]string{"select-tests", "-since", "HEAD", "-format", format, "-cache", cachePath, dir}, &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}
	out, errOut, code := selectTests("json")
	if code != 0 {
		t.Fatalf("select-tests: exit %d:\n%s", code, errOut)
	}
	var got struct {
		Packages []struct {
			Package string `json:"package"`
			Tests   []struct {
				Name    string `json:"name"`
				Group   int    `json:"group"`
				Kills   int    `json:"kills"`
				Reaches int    `json:"reaches"`
				Reason  string `json:"reason"`
			} `json:"tests"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if len(got.Packages) != 1 || got.Packages[0].Package != "selecttests" {
		t.Fatalf("packages %+v", got.Packages)
	}
	var order []string
	for _, s := range got.Packages[0].Tests {
		order = append(order, s.Name)
		if s.Reason == "" {
			t.Errorf("%s has no reason", s.Name)
		}
	}
	want := []string{"TestAddBig", "TestAddSmall", "TestAddFive", "TestAddZero", "TestMul"}
	if strings.Join(order, " ") != strings.Join(want, " ") {
		t.Errorf("order %v, want %v", order, want)
	}
	tests := got.Packages[0].Tests
	if len(tests) == len(want) && (tests[0].Kills != 2 || tests[2].Kills != 1 || tests[3].Group != 2 || tests[3].Reaches == 0 || tests[4].Group != 3) {
		t.Errorf("groups and counts %+v", tests)
	}

	out, _, code = selectTests("run")
	if code != 0 || strings.TrimSpace(out) != "selecttests\t^(?:TestAddBig|TestAddSmall|TestAddFive|TestAddZero|TestMul)$" {
		t.Errorf("run format: exit %d:\n%s", code, out)
	}

	var noCache strings.Builder
	if code := realMain([]string{"select-tests", "-since", "HEAD", "-cache", filepath.Join(t.TempDir(), "none.json"), dir}, &noCache, &noCache); code != exitError {
		t.Errorf("without a cache: exit %d, want %d:\n%s", code, exitError, noCache.String())
	}
}

// edit は、path の from を to に置き換える。
func edit(t *testing.T, path, from, to string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), from) {
		t.Fatalf("%s lacks %q", path, from)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), from, to, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
}
