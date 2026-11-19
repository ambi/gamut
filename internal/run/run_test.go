package run_test

import (
	"context"
	"fmt"
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
	results, err := run.Run(context.Background(), pkg, mutants, run.Options{
		Workers: 2, TimeoutFactor: 5, TimeoutMin: time.Second,
	})
	if err != nil {
		t.Fatal(err)
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
