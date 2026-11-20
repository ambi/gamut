package load_test

import (
	"slices"
	"testing"

	"github.com/ambi/gamut/internal/load"
)

// TestWithHint_HintNeedsNoTargetExportUnlessImported は、エクスポートデータを求めるパッケージ（次回の hint）が、
// 依存と、別の対象パッケージが import する対象パッケージだけであることを確かめる。
// multi では b が a を import するので、a は含み、b と c は含まない。
func TestWithHint_HintNeedsNoTargetExportUnlessImported(t *testing.T) {
	_, hint, err := load.WithHint([]string{"../../testdata/multi/..."}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(hint, "multi/a") {
		t.Errorf("hint %v lacks multi/a, which multi/b imports", hint)
	}
	for _, path := range []string{"multi/b", "multi/c"} {
		if slices.Contains(hint, path) {
			t.Errorf("hint %v contains %s, which no target imports", hint, path)
		}
	}
	if !slices.Contains(hint, "sync/atomic") {
		t.Errorf("hint %v lacks the helper import sync/atomic", hint)
	}
}

// TestWithHint_StaleHintGivesTheSameTypes は、前回の hint、古い hint（ないパッケージを含む）、hint なしのどれでも、同じ型の検査の結果になることを確かめる。
func TestWithHint_StaleHintGivesTheSameTypes(t *testing.T) {
	patterns := []string{"../../testdata/multi/..."}
	base, hint, err := load.WithHint(patterns, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, h := range map[string][]string{
		"previous": hint,
		"stale":    append([]string{"no/such/package"}, hint[:len(hint)/2]...),
	} {
		t.Run(name, func(t *testing.T) {
			pkgs, again, err := load.WithHint(patterns, nil, h)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(again, hint) {
				t.Errorf("hint %v, want %v", again, hint)
			}
			if len(pkgs) != len(base) {
				t.Fatalf("%d packages, want %d", len(pkgs), len(base))
			}
			for i, p := range pkgs {
				if got, want := p.Types.Scope().Names(), base[i].Types.Scope().Names(); !slices.Equal(got, want) {
					t.Errorf("%s: scope %v, want %v", p.ImportPath, got, want)
				}
			}
		})
	}
}
