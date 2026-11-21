package main

import (
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
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
			rep := readMTE(t, reportPath)
			files := slices.Sorted(maps.Keys(rep.Files))
			for _, m := range rep.mutants() {
				if m.Status != "Killed" {
					t.Errorf("%s is %s, want Killed", m.ID, m.Status)
				}
			}
			if strings.Join(files, " ") != strings.Join(tc.files, " ") {
				t.Errorf("files %v, want %v", files, tc.files)
			}
			skipped := rep.Gamut.SkippedPackages
			if got := len(skipped) == 1 && skipped[0].Package == "cgo/c"; got != tc.skippedCgoC {
				t.Errorf("skipped packages %v, want cgo/c skipped %t", skipped, tc.skippedCgoC)
			}
			if tc.skippedCgoC && !strings.Contains(stdout.String(), "cgo/c") {
				t.Errorf("the summary does not name the skipped package:\n%s", stdout.String())
			}
		})
	}
}
