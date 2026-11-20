package run_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

func loadAndRun(t *testing.T, dir string, tags []string, opts run.Options) ([]*run.Result, error) {
	t.Helper()
	pkg, err := loadOne(dir, tags)
	if err != nil {
		t.Fatal(err)
	}
	mutants := mutant.Enumerate(pkg, mutant.AllTypes)
	mutant.CheckViability(pkg, mutants)
	if opts.Workers == 0 {
		opts.Workers = 2
	}
	if opts.TimeoutFactor == 0 {
		opts.TimeoutFactor, opts.TimeoutMin = 5, time.Second
	}
	return run.Run(context.Background(), pkg, mutants, opts)
}

func statusByFunc(results []*run.Result) map[string]run.Status {
	out := map[string]run.Status{}
	for _, r := range results {
		out[r.Mutant.Scope+" "+string(r.Mutant.Type)] = r.Status
	}
	return out
}

func TestLoad_BuildTagsSelectFiles(t *testing.T) {
	enumerated := func(tags []string) map[string]bool {
		pkg, err := loadOne("../../testdata/tags", tags)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, m := range mutant.Enumerate(pkg, mutant.AllTypes) {
			out[m.Scope] = true
		}
		return out
	}
	if got := enumerated(nil); !got["Inc"] || got["Double"] {
		t.Errorf("without tags: functions with mutants = %v, want only Inc", got)
	}
	if got := enumerated([]string{"extra"}); !got["Inc"] || !got["Double"] {
		t.Errorf("with -tags extra: functions with mutants = %v, want Inc and Double", got)
	}
}

func TestRun_PassesBuildTagsToTheTestBinary(t *testing.T) {
	results, err := loadAndRun(t, "../../testdata/tags", []string{"extra"}, run.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// タグ付きのテストがビルドに入らなければ、Double の変異体はどのテストも到達しない。
	if got := statusByFunc(results)["Double "+string(mutant.ArithmeticBase)]; got != run.Killed {
		t.Errorf("Double ARITHMETIC_BASE = %s, want KILLED", got)
	}
}

func TestRun_PassesTestFlagsToEveryRun(t *testing.T) {
	flags, err := run.ParseTestFlags("-short")
	if err != nil {
		t.Fatal(err)
	}
	results, err := loadAndRun(t, "../../testdata/flags", nil, run.Options{TestFlags: flags})
	if err != nil {
		t.Fatal(err)
	}
	if got := statusByFunc(results)["Half "+string(mutant.ArithmeticBase)]; got != run.Killed {
		t.Errorf("Half ARITHMETIC_BASE = %s, want KILLED", got)
	}
}

func TestRun_StopsWithTestOutputWhenBaselineFails(t *testing.T) {
	results, err := loadAndRun(t, "../../testdata/flags", nil, run.Options{})
	if !errors.Is(err, run.ErrBaseline) {
		t.Fatalf("err = %v, want ErrBaseline", err)
	}
	if !strings.Contains(err.Error(), "this test needs -short") {
		t.Errorf("the error must show the failing test's output, got: %v", err)
	}
	if results != nil {
		t.Errorf("no verdicts may be returned when the baseline fails, got %d", len(results))
	}
}

func TestParseTestFlags(t *testing.T) {
	cases := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{"", nil, false},
		{"-short -v", []string{"-test.short", "-test.v"}, false},
		{"-test.short -timeout=30s", []string{"-test.short", "-test.timeout=30s"}, false},
		{"--short", []string{"-test.short"}, false},
		// テストが自分で定義したフラグはそのまま渡す。
		{"-update -short", []string{"-update", "-test.short"}, false},
		// gamut が実行ごとに決めるフラグは受け付けない。
		{"-run TestX", nil, true},
		{"-test.failfast", nil, true},
		{"-count=2", nil, true},
	}
	for _, c := range cases {
		got, err := run.ParseTestFlags(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ParseTestFlags(%q) err = %v, wantErr %t", c.in, err, c.wantErr)
			continue
		}
		if !c.wantErr && strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("ParseTestFlags(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRun_TimeoutKillsChildProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups are Unix only")
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("ORPHAN_PIDFILE", pidFile)
	results, err := loadAndRun(t, "../../testdata/orphan", nil, run.Options{TimeoutFactor: 1, TimeoutMin: 500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if got := statusByFunc(results)["Ready "+string(mutant.ConditionalsNegation)]; got != run.TimedOut {
		t.Fatalf("Ready CONDITIONALS_NEGATION = %s, want TIMED OUT", got)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the mutant never started the child process: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	// 子プロセスはグループごと SIGKILL を受け、init に回収されるまで少し待つ。
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child process %d is still running after the timeout", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRun_RecordsCoverageFromConcurrentTests(t *testing.T) {
	results, err := loadAndRun(t, "../../testdata/concurrent", nil, run.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != run.Killed {
			t.Errorf("%s %s = %s %s, want KILLED", r.Mutant.Scope, r.Mutant.Type, r.Status, r.Reason)
		}
	}
}
