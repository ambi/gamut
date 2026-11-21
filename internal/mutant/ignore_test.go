package mutant_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/mutant"
)

func TestIgnores_AppliesEachCommentForm(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// ignored は「行 演算子 理由」の並びである。
		ignored  []string
		warnings int
	}{
		{name: "a trailing comment covers its own line",
			src:     "package m\n\nfunc F(a, b int) int {\n\treturn a + b //gamut:ignore -- the sum is only a cache key\n}\n\nfunc G(a, b int) int { return a - b }\n",
			ignored: []string{"4 ARITHMETIC_BASE the sum is only a cache key"}},
		{name: "a comment on its own line covers the next line, filtered by type",
			src:     "package m\n\nfunc F(a, b int) bool {\n\t//gamut:ignore CONDITIONALS_BOUNDARY -- equal values never reach here\n\treturn a < b\n}\n",
			ignored: []string{"5 CONDITIONALS_BOUNDARY equal values never reach here"}},
		{name: "a comment before a function covers the whole function",
			src: "package m\n\n// F は速さのための近道である。\n//gamut:ignore -- a fast path with the same result\nfunc F(a, b int) int {\n\tif a < b {\n\t\treturn a + b\n\t}\n\treturn a - b\n}\n\nfunc G(a, b int) int { return a * b }\n",
			ignored: []string{
				"6 CONDITIONALS_BOUNDARY a fast path with the same result",
				"6 CONDITIONALS_NEGATION a fast path with the same result",
				"7 ARITHMETIC_BASE a fast path with the same result",
				"9 ARITHMETIC_BASE a fast path with the same result",
			}},
		{name: "a range covers the lines between start and end",
			src:     "package m\n\nfunc F(a, b int) int {\n\t//gamut:ignore-start ARITHMETIC_BASE -- defensive arithmetic\n\tx := a + b\n\ty := a - b\n\t//gamut:ignore-end\n\treturn x * y\n}\n",
			ignored: []string{"5 ARITHMETIC_BASE defensive arithmetic", "6 ARITHMETIC_BASE defensive arithmetic"}},
		{name: "a comment without a reason still applies and warns",
			src:      "package m\n\nfunc F(a, b int) int {\n\treturn a + b //gamut:ignore\n}\n",
			ignored:  []string{"4 ARITHMETIC_BASE "},
			warnings: 1},
		{name: "an unterminated range runs to the end of the file and warns",
			src:      "package m\n\n//gamut:ignore-start -- generated-like tables\nfunc F(a, b int) int { return a + b }\n\nfunc G(a, b int) int { return a - b }\n",
			ignored:  []string{"4 ARITHMETIC_BASE generated-like tables", "6 ARITHMETIC_BASE generated-like tables"},
			warnings: 1},
		{name: "an end without a start and an unknown directive warn",
			src:      "package m\n\n//gamut:ignore-end\n//gamut:ignoer -- typo\nfunc F(a, b int) int { return a + b }\n",
			warnings: 2},
		{name: "a plain comment mentioning the directive is not one",
			src: "package m\n\n// gamut:ignore は、空白を挟むと指令にならない。\nfunc F(a, b int) int { return a + b }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := loadOne(writeModule(t, tc.src))
			if err != nil {
				t.Fatal(err)
			}
			all := mutant.Enumerate(pkg, classic)
			total := len(all)
			kept, ignored, warnings, err := mutant.Ignores(pkg, all)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, ig := range ignored {
				got = append(got, fmt.Sprintf("%d %s %s", ig.Mutant.Line, ig.Mutant.Type, ig.Reason))
			}
			slices.Sort(got)
			want := slices.Sorted(slices.Values(tc.ignored))
			if !slices.Equal(got, want) {
				t.Errorf("ignored:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
			}
			if len(kept)+len(ignored) != total {
				t.Errorf("%d kept + %d ignored, want %d in all", len(kept), len(ignored), total)
			}
			for i, m := range kept {
				if m.Index != i+1 {
					t.Errorf("kept mutant %s has index %d, want %d", m.ID, m.Index, i+1)
				}
			}
			if len(warnings) != tc.warnings {
				t.Errorf("warnings %q, want %d", warnings, tc.warnings)
			}
		})
	}
}

func TestIgnores_RejectsUnknownMutatorTypes(t *testing.T) {
	pkg, err := loadOne(writeModule(t, "package m\n\nfunc F(a, b int) int {\n\treturn a + b //gamut:ignore ARITHMETIC -- typo\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := mutant.Ignores(pkg, mutant.Enumerate(pkg, classic)); err == nil || !strings.Contains(err.Error(), "ARITHMETIC") {
		t.Errorf("Ignores() error = %v, want one naming ARITHMETIC", err)
	}
}
