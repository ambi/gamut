package main

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"
)

// twoFiles creates a module with mutation sites in two files, both reached by tests.
func twoFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "go.mod", "module two\n\ngo 1.27.4\n")
	write(t, dir, "a.go", "package two\n\nfunc Add(a, b int) int { return a + b }\n\nfunc Less(a, b int) bool { return a < b }\n")
	write(t, dir, "b.go", "package two\n\nfunc Sub(a, b int) int { return a - b }\n")
	write(t, dir, "two_test.go", `package two

import "testing"

func TestAll(t *testing.T) {
	if Add(1, 2) != 3 || Sub(3, 1) != 2 || !Less(1, 2) {
		t.Fatal("wrong")
	}
}
`)
	return dir
}

func statuses(ms []mutation) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		out[m.ID] = m.Status
	}
	return out
}

func TestConfig_ItemsMatchTheirFlags(t *testing.T) {
	dir := twoFiles(t)
	byFlags, _ := mustGamut(t, "-no-cache", "-only", "ARITHMETIC_BASE", "-exclude-files", `^b\.go$`, "-timeout-factor", "4", "-timeout-min", "3s", dir)
	write(t, dir, "gamut.json", `{"mutators": ["ARITHMETIC_BASE"], "exclude_files": ["^b\\.go$"], "timeout": {"factor": 4, "min": "3s"}}`)
	byConfig, stderr := mustGamut(t, "-no-cache", dir)
	if !strings.Contains(stderr, "config: "+filepath.Join(dir, "gamut.json")) {
		t.Errorf("want the config file named in the log, log:\n%s", stderr)
	}
	if len(byFlags) == 0 {
		t.Fatal("no mutants")
	}
	for _, m := range byConfig {
		if m.Type != "ARITHMETIC_BASE" || strings.HasPrefix(m.ID, "b.go::") {
			t.Errorf("%s (%s) is not excluded", m.ID, m.Type)
		}
	}
	if got, want := statuses(byConfig), statuses(byFlags); !maps.Equal(got, want) {
		t.Errorf("config verdicts %v, flag verdicts %v", got, want)
	}
}

func TestConfig_FlagsOverrideTheFile(t *testing.T) {
	dir := twoFiles(t)
	write(t, dir, "gamut.json", `{"mutators": ["CONDITIONALS_NEGATION"], "exclude_files": ["^a\\.go$"]}`)
	ms, _ := mustGamut(t, "-no-cache", "-only", "ARITHMETIC_BASE", "-exclude-files", `^b\.go$`, dir)
	if len(ms) == 0 {
		t.Fatal("no mutants")
	}
	for _, m := range ms {
		if m.Type != "ARITHMETIC_BASE" || !strings.HasPrefix(m.ID, "a.go::") {
			t.Errorf("%s (%s): want only ARITHMETIC_BASE in a.go", m.ID, m.Type)
		}
	}
}

func TestConfig_ExplicitPathAndErrors(t *testing.T) {
	dir := twoFiles(t)
	other := t.TempDir()
	write(t, other, "custom.json", `{"mutators": ["CONDITIONALS_BOUNDARY"]}`)
	ms, _ := mustGamut(t, "-no-cache", "-config", filepath.Join(other, "custom.json"), dir)
	if len(ms) == 0 {
		t.Fatal("no mutants")
	}
	for _, m := range ms {
		if m.Type != "CONDITIONALS_BOUNDARY" {
			t.Errorf("%s (%s): want only CONDITIONALS_BOUNDARY from -config", m.ID, m.Type)
		}
	}
	cases := []struct {
		name string
		args []string
		file string
	}{
		{"unknown item", nil, `{"mutatorz": ["ARITHMETIC_BASE"]}`},
		{"missing explicit file", []string{"-config", filepath.Join(other, "none.json")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := twoFiles(t)
			if tc.file != "" {
				write(t, d, "gamut.json", tc.file)
			}
			if _, stderr, code := gamut(t, append(append([]string{"-no-cache"}, tc.args...), d)...); code != 1 {
				t.Errorf("exit %d, want 1:\n%s", code, stderr)
			}
		})
	}
}
