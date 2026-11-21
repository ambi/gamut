package accept_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/accept"
)

const (
	idA = "a.go::ARITHMETIC_BASE::F::0011223344"
	idB = "pkg/b.go::CONDITIONALS_BOUNDARY::G::5566778899"
)

func TestFile_AddSaveLoadRemove(t *testing.T) {
	dir := t.TempDir()
	f, err := accept.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Mutants) != 0 {
		t.Fatalf("a missing file loads %v, want empty", f.Mutants)
	}
	if err := f.Add([]string{idA, idB}, "equivalent: the sum is only a cache key"); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := accept.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Reason(idB); got != "equivalent: the sum is only a cache key" {
		t.Errorf("Reason(idB) = %q", got)
	}
	again.Remove([]string{idA})
	if err := again.Save(); err != nil {
		t.Fatal(err)
	}
	last, err := accept.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if last.Reason(idA) != "" || last.Reason(idB) == "" {
		t.Errorf("after removing idA: %v", last.Mutants)
	}
	data, err := os.ReadFile(filepath.Join(dir, accept.FileName))
	if err != nil {
		t.Fatal(err)
	}
	// 差分で読みやすいよう、項目ごとに行を分けて、識別子の順に書く。
	if !strings.Contains(string(data), "\n    \""+idB+"\": {\n") {
		t.Errorf("unexpected layout:\n%s", data)
	}
}

func TestFile_RejectsMalformedInput(t *testing.T) {
	dir := t.TempDir()
	f, err := accept.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Add([]string{idA}, "  "); err == nil {
		t.Errorf("Add with an empty reason succeeded")
	}
	if err := f.Add([]string{"not-an-id"}, "r"); err == nil || !strings.Contains(err.Error(), "not-an-id") {
		t.Errorf("Add(not-an-id) error = %v, want one naming the id", err)
	}
	if err := os.WriteFile(filepath.Join(dir, accept.FileName), []byte(`{"version": 1, "mutantz": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := accept.Load(dir); err == nil || !strings.Contains(err.Error(), "mutantz") {
		t.Errorf("Load of an unknown item: %v", err)
	}
}

func TestInPackages_MatchesTheIDsFileDirectory(t *testing.T) {
	cases := []struct {
		id   string
		dirs []string
		want bool
	}{
		{idA, []string{"."}, true},
		{idB, []string{"pkg"}, true},
		{idB, []string{"."}, false},
		{"pkg/sub/c.go::ARITHMETIC_BASE::H::00", []string{"pkg"}, false},
	}
	for _, tc := range cases {
		if got := accept.InPackages(tc.id, tc.dirs); got != tc.want {
			t.Errorf("InPackages(%q, %v) = %t, want %t", tc.id, tc.dirs, got, tc.want)
		}
	}
}
