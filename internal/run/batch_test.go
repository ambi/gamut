package run_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

// TestAll_BatchedCoverageMatchesOneProcessPerTest は、1 つのプロセスでまとめて記録した被覆が、テスト 1 件ずつのプロセスの記録と同じ判定と到達のテストを生むことを確かめる。
// まとめる方法は 2 つある。判定のテストのプロセスで測ってそのまま判定に使い回す方法（既定）と、-isolate のときの go test の 1 回の実行である。
// concurrent のテストは t.Parallel を呼び、多数のゴルーチンから変異箇所を通る。
func TestAll_BatchedCoverageMatchesOneProcessPerTest(t *testing.T) {
	for _, dir := range []string{"../../testdata/fixture", "../../testdata/concurrent", "../../testdata/reuse", "../../testdata/ordering"} {
		t.Run(dir, func(t *testing.T) {
			pkg, err := loadOne(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			mutants := mutant.Enumerate(pkg, mutant.AllTypes)
			mutant.CheckViability(pkg, mutants)
			opts := run.Options{Workers: 2, TimeoutFactor: 5, TimeoutMin: time.Second}
			var log bytes.Buffer
			batched := opts
			batched.Log = &log
			got, err := run.Run(context.Background(), pkg, mutants, batched)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(log.String(), "(0 one by one)") {
				t.Errorf("want every test measured in batches, log:\n%s", log.String())
			}
			want, err := run.Run(context.Background(), pkg, mutants, run.NoBatch(opts))
			if err != nil {
				t.Fatal(err)
			}
			isolated := opts
			isolated.Isolate = true
			alone, err := run.Run(context.Background(), pkg, mutants, isolated)
			if err != nil {
				t.Fatal(err)
			}
			for i, g := range alone {
				w := want[i]
				if g.Status != w.Status || g.Reason != w.Reason || !slices.Equal(sorted(g.Tests), sorted(w.Tests)) {
					t.Errorf("%s: batched in go test %s/%s %v, one by one %s/%s %v", g.Mutant.ID, g.Status, g.Reason, g.Tests, w.Status, w.Reason, w.Tests)
				}
			}
			for i, g := range got {
				w := want[i]
				if g.Status != w.Status || g.Reason != w.Reason || !slices.Equal(sorted(g.Tests), sorted(w.Tests)) {
					t.Errorf("%s: batched %s/%s %v, one by one %s/%s %v", g.Mutant.ID, g.Status, g.Reason, g.Tests, w.Status, w.Reason, w.Tests)
				}
			}
		})
	}
}

func sorted(s []string) []string {
	return slices.Sorted(slices.Values(s))
}

// TestAll_ServedVerdictsMatchOneProcessPerMutant は、テストプロセスを変異体の間で使い回す判定が、変異体ごとに新しいプロセスで判定したものと一致することを確かめる。
// fixture は時間切れと panic で落ちる変異体を、concurrent は t.Parallel のテストを含む。
func TestAll_ServedVerdictsMatchOneProcessPerMutant(t *testing.T) {
	for _, dir := range []string{"../../testdata/fixture", "../../testdata/concurrent", "../../testdata/reuse", "../../testdata/ordering"} {
		t.Run(dir, func(t *testing.T) {
			pkg, err := loadOne(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			mutants := mutant.Enumerate(pkg, mutant.AllTypes)
			mutant.CheckViability(pkg, mutants)
			opts := run.Options{Workers: 2, TimeoutFactor: 5, TimeoutMin: time.Second}
			served, err := run.Run(context.Background(), pkg, mutants, opts)
			if err != nil {
				t.Fatal(err)
			}
			opts.Isolate = true
			isolated, err := run.Run(context.Background(), pkg, mutants, opts)
			if err != nil {
				t.Fatal(err)
			}
			for i, g := range served {
				w := isolated[i]
				if g.Status != w.Status || g.Reason != w.Reason || g.Mode != w.Mode {
					t.Errorf("%s: served %s/%s/%s, isolated %s/%s/%s", g.Mutant.ID, g.Status, g.Reason, g.Mode, w.Status, w.Reason, w.Mode)
				}
			}
		})
	}
}
