package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMaxDepth は、-max-depth を付けると、深い呼び出しの先でしか変異箇所に到達しないテストを、到達したテストから除くことを確かめる。
// サブテストの関数から数えることと、除いた数を報告することも、被覆の記録をキャッシュから戻した実行で確かめる。
func TestMaxDepth(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		tests    []string
		excluded int
	}{
		{nil, []string{"TestLeaf", "TestTop"}, 0},
		{[]string{"-max-depth", "2"}, []string{"TestLeaf"}, 1},
		{[]string{"-max-depth", "3"}, []string{"TestLeaf", "TestTop"}, 0},
	} {
		t.Run(strings.Join(append([]string{"depth"}, tc.args...), " "), func(t *testing.T) {
			dir := copyModule(t, "../../testdata/depth")
			cachePath := filepath.Join(t.TempDir(), "cache.json")
			for _, run := range []string{"measuring", "restoring"} {
				if run == "restoring" {
					// 中身を変えずに更新時刻だけを変え、変更のない再実行の近道を外す。被覆の記録は戻る。
					later := time.Now().Add(time.Minute)
					if err := os.Chtimes(filepath.Join(dir, "depth_test.go"), later, later); err != nil {
						t.Fatal(err)
					}
				}
				reportPath := filepath.Join(t.TempDir(), "r.json")
				var stdout, stderr strings.Builder
				args := slices.Concat([]string{"-only", "ARITHMETIC_BASE", "-o", reportPath, "-cache", cachePath}, tc.args, []string{dir})
				if code := realMain(args, &stdout, &stderr); code != 0 {
					t.Fatalf("%s: exit %d:\n%s", run, code, stderr.String())
				}
				if run == "restoring" && !strings.Contains(stderr.String(), "2 restored from the cache") {
					t.Errorf("coverage was not restored:\n%s", stderr.String())
				}
				data, err := os.ReadFile(reportPath)
				if err != nil {
					t.Fatal(err)
				}
				var rep struct {
					Excluded int `json:"max_depth_excluded_tests"`
					Files    []struct {
						Mutations []mutation `json:"mutations"`
					} `json:"files"`
				}
				if err := json.Unmarshal(data, &rep); err != nil {
					t.Fatal(err)
				}
				var tests []string
				for _, f := range rep.Files {
					for _, m := range f.Mutations {
						tests = append(tests, m.Tests...)
					}
				}
				slices.Sort(tests)
				if !slices.Equal(tests, tc.tests) {
					t.Errorf("%s: reached by %v, want %v", run, tests, tc.tests)
				}
				if rep.Excluded != tc.excluded {
					t.Errorf("%s: max_depth_excluded_tests %d, want %d", run, rep.Excluded, tc.excluded)
				}
				if tc.excluded > 0 && !strings.Contains(stdout.String(), "beyond -max-depth") {
					t.Errorf("%s: the summary does not mention -max-depth:\n%s", run, stdout.String())
				}
			}
		})
	}
}
