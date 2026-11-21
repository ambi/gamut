package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCgo は、cgo を使うパッケージの、import "C" を持たないファイルを判定し、持つファイルを変異しないことを確かめる。
// C コンパイラが使えなければ、cgo のパッケージを飛ばして報告し、ほかのパッケージの判定を続ける。
func TestCgo(t *testing.T) {
	cc, err := exec.Command("go", "env", "CC").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, cc    string
		files       []string
		skippedCgoC bool
	}{
		{"with a C compiler", strings.TrimSpace(string(cc)), []string{"c/pure.go", "plain/plain.go"}, false},
		{"without a C compiler", filepath.Join(t.TempDir(), "no-such-cc"), []string{"plain/plain.go"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := exec.LookPath(tc.cc); !tc.skippedCgoC && err != nil {
				t.Skipf("no C compiler: %v", err)
			}
			t.Setenv("CC", tc.cc)
			reportPath := filepath.Join(t.TempDir(), "r.json")
			var stdout, stderr strings.Builder
			if code := realMain([]string{"-no-cache", "-o", reportPath, "../../testdata/cgo/..."}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit %d:\n%s", code, stderr.String())
			}
			data, err := os.ReadFile(reportPath)
			if err != nil {
				t.Fatal(err)
			}
			var rep struct {
				Skipped []struct{ Package, Reason string } `json:"skipped_packages"`
				Files   []struct {
					Path      string     `json:"path"`
					Mutations []mutation `json:"mutations"`
				} `json:"files"`
			}
			if err := json.Unmarshal(data, &rep); err != nil {
				t.Fatal(err)
			}
			var files []string
			for _, f := range rep.Files {
				files = append(files, f.Path)
				for _, m := range f.Mutations {
					if m.Status != "KILLED" {
						t.Errorf("%s is %s, want KILLED", m.ID, m.Status)
					}
				}
			}
			if strings.Join(files, " ") != strings.Join(tc.files, " ") {
				t.Errorf("files %v, want %v", files, tc.files)
			}
			if got := len(rep.Skipped) == 1 && rep.Skipped[0].Package == "cgo/c"; got != tc.skippedCgoC {
				t.Errorf("skipped_packages %v, want cgo/c skipped %t", rep.Skipped, tc.skippedCgoC)
			}
			if tc.skippedCgoC && !strings.Contains(stdout.String(), "cgo/c") {
				t.Errorf("the summary does not name the skipped package:\n%s", stdout.String())
			}
		})
	}
}
