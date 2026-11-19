package run_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

// verdict は、行と列と演算子で変異体を指し、期待する判定を並べる。
type verdict struct {
	status run.Status
	reason string
	mode   string
}

func TestRun_ClassifiesEveryVerdictOnFixture(t *testing.T) {
	pkg, err := load.Load("../../testdata/fixture")
	if err != nil {
		t.Fatal(err)
	}
	mutants := mutant.Enumerate(pkg, mutant.AllTypes)
	mutant.CheckViability(pkg, mutants)
	var log bytes.Buffer
	results, err := run.Run(context.Background(), pkg, mutants, run.Options{
		Workers: 2, TimeoutFactor: 5, TimeoutMin: time.Second, Log: &log,
	})
	if err != nil {
		t.Fatal(err)
	}
	// forced fail の検査は、到達のあるテスト 1 件を 1 回のプロセス起動で実行して終わる。
	if got := regexp.MustCompile(`(?m)^self-check: forced fail failed Test\w+ in `).FindAllString(log.String(), -1); len(got) != 1 {
		t.Errorf("want exactly one forced-fail run naming one test, got log:\n%s", log.String())
	}

	want := map[string]verdict{
		"10:7 CONDITIONALS_BOUNDARY":  {run.Lived, run.ReasonNotInfected, run.ModeSchemata},
		"10:7 CONDITIONALS_NEGATION":  {run.Killed, "", run.ModeSchemata},
		"19:16 CONDITIONALS_BOUNDARY": {run.Killed, "", run.ModeSchemata},
		"19:16 CONDITIONALS_NEGATION": {run.Killed, "", run.ModeSchemata},
		"19:28 INCREMENT_DECREMENT":   {run.Killed, "", run.ModeSchemata},
		"20:17 ARITHMETIC_BASE":       {run.Killed, "", run.ModeSchemata},
		"26:33 INVERT_NEGATIVES":      {run.Killed, "", run.ModeSchemata},
		"30:14 CONDITIONALS_NEGATION": {run.Killed, "", run.ModeSchemata},
		// 短絡評価で右辺が評価されないので、同じ行でも到達しない。
		"30:26 CONDITIONALS_NEGATION": {run.NotCovered, "", run.ModeSchemata},
		"34:38 ARITHMETIC_BASE":       {run.NotCovered, "", run.ModeSchemata},
		// 定数の文脈は schemata に埋め込めず、個別ビルドで全テストを実行する。
		"37:19 ARITHMETIC_BASE":       {run.Lived, run.ReasonNotDetected, run.ModeStandalone},
		"48:16 CONDITIONALS_BOUNDARY": {run.Killed, "", run.ModeSchemata},
		"48:16 CONDITIONALS_NEGATION": {run.Killed, "", run.ModeSchemata},
		"48:22 INCREMENT_DECREMENT":   {run.TimedOut, "", run.ModeSchemata},
		"49:4 INCREMENT_DECREMENT":    {run.Killed, "", run.ModeSchemata},
	}
	got := map[string]verdict{}
	for _, r := range results {
		key := fmt.Sprintf("%d:%d %s", r.Mutant.Line, r.Mutant.Column, r.Mutant.Type)
		got[key] = verdict{r.Status, r.Reason, r.Mode}
	}
	for key, w := range want {
		if g, ok := got[key]; !ok {
			t.Errorf("%s: missing", key)
		} else if g != w {
			t.Errorf("%s: got %+v, want %+v", key, g, w)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("%s: unexpected mutant (log arguments must not be mutated)", key)
		}
	}
}

func TestRun_CanaryVerdicts(t *testing.T) {
	pkg, err := load.Load("../../testdata/canary")
	if err != nil {
		t.Fatal(err)
	}
	mutants := mutant.Enumerate(pkg, []mutant.Type{mutant.ArithmeticBase, mutant.ConditionalsBoundary})
	mutant.CheckViability(pkg, mutants)
	results, err := run.Run(context.Background(), pkg, mutants, run.Options{Workers: 1, TimeoutFactor: 5, TimeoutMin: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[mutant.Type]run.Status{}
	for _, r := range results {
		statuses[r.Mutant.Type] = r.Status
	}
	if statuses[mutant.ConditionalsBoundary] != run.Lived || statuses[mutant.ArithmeticBase] != run.NotViable {
		t.Fatalf("canary verdicts = %v, want boundary LIVED and arithmetic NOT VIABLE", statuses)
	}
}

func TestRun_StopsWhenSelectionDoesNotReachTests(t *testing.T) {
	pkg, err := load.Load("../../testdata/fixture")
	if err != nil {
		t.Fatal(err)
	}
	mutants := mutant.Enumerate(pkg, mutant.AllTypes)
	mutant.CheckViability(pkg, mutants)
	opts := run.IgnoreSelection(run.Options{Workers: 2, TimeoutFactor: 5, TimeoutMin: time.Second})
	results, err := run.Run(context.Background(), pkg, mutants, opts)
	if !errors.Is(err, run.ErrSelfCheck) {
		t.Fatalf("err = %v, want ErrSelfCheck", err)
	}
	if !strings.Contains(err.Error(), "GAMUT_MUTANT") {
		t.Errorf("error must explain the cause, got: %v", err)
	}
	if results != nil {
		t.Errorf("no verdicts may be returned when the self-check fails, got %d", len(results))
	}
}

func TestRun_WarnsWhenNoTestReachesAnySite(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":    "module m\n\ngo 1.27.4\n",
		"m.go":      "package m\n\nfunc Add(a, b int) int { return a + b }\n",
		"m_test.go": "package m\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := load.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	mutants := mutant.Enumerate(pkg, mutant.AllTypes)
	mutant.CheckViability(pkg, mutants)
	var warn bytes.Buffer
	results, err := run.Run(context.Background(), pkg, mutants, run.Options{
		Workers: 1, TimeoutFactor: 5, TimeoutMin: time.Second, Warn: &warn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warn.String(), "forced-fail self-check skipped") {
		t.Errorf("want a skip warning, got %q", warn.String())
	}
	for _, r := range results {
		if r.Status != run.NotCovered {
			t.Errorf("%s: status %s, want NOT COVERED", r.Mutant.ID, r.Status)
		}
	}
}
