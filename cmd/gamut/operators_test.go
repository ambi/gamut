package main

import (
	"slices"
	"testing"

	"github.com/ambi/gamut/internal/mutant"
)

// TestOperators_OptInVerdicts は、既定では無効な変異演算子を -only で選ぶと列挙され、固定モジュールで期待どおりに判定されることを確かめる。
// 既定の集合では、既定で無効な演算子の変異体が列挙されないことも確かめる。
func TestOperators_OptInVerdicts(t *testing.T) {
	dir := "../../testdata/operators"
	cases := []struct {
		only string
		want map[string]string // 囲む宣言 -> 状態
	}{
		{"INVERT_BITWISE", map[string]string{"Flags": "Killed", "Mask": "Survived", "Double": "Killed"}},
		{"INVERT_ASSIGNMENTS", map[string]string{"Total": "Killed", "Scaled": "Survived"}},
		{"INVERT_BITWISE_ASSIGNMENTS", map[string]string{"SetBit": "Killed", "ClearHigh": "Survived"}},
		{"INVERT_LOOP_CTRL", map[string]string{"FirstNegative": "Killed", "CountPositive": "Survived"}},
		{"INVERT_LOGICAL", map[string]string{"Both": "Killed", "Either": "Survived"}},
		{"REMOVE_LOGICAL_NOT", map[string]string{"Closed": "Killed", "Muted": "Survived"}},
		{"BOOLEAN_LITERAL", map[string]string{"Enabled": "Killed", "Verbose": "Survived"}},
		{"CONDITIONS_CONSTANT", map[string]string{"Positive": "Killed", "SumAll": "Survived"}},
		{"STATEMENT_REMOVE", map[string]string{"Record": "Killed", "Notify": "Survived"}},
		{"RETURN_ZERO", map[string]string{"Area": "Killed", "Describe": "Survived"}},
		{"REMOVE_SELF_ASSIGNMENTS", map[string]string{"Total": "Killed", "Accumulate": "Survived"}},
		{"INTEGER_INCREMENT", map[string]string{"Limit": "Killed", "Retries": "Survived"}},
		{"INTEGER_DECREMENT", map[string]string{"Limit": "Killed", "Retries": "Survived"}},
		{"FLOAT_INCREMENT", map[string]string{"Rate": "Killed", "Ratio": "Survived"}},
		{"FLOAT_DECREMENT", map[string]string{"Rate": "Killed", "Ratio": "Survived"}},
		{"BRANCH_IF", map[string]string{"Abs": "Killed", "Note": "Survived"}},
		{"BRANCH_ELSE", map[string]string{"Sign": "Killed", "Level": "Survived"}},
		{"BRANCH_CASE", map[string]string{"Name": "Killed", "Kind": "Survived"}},
		{"ERRORF_WRAP", map[string]string{"Wrap": "Killed", "Annotate": "Survived"}},
		{"RETURN_ERROR_NIL", map[string]string{"Check": "Killed", "Validate": "Survived"}},
	}
	for _, tc := range cases {
		t.Run(tc.only, func(t *testing.T) {
			ms, _ := mustGamut(t, "-no-cache", "-only", tc.only, dir)
			// 挙げた宣言のすべての変異体が、期待する状態を持つ。ほかの宣言の変異体は数えない。
			seen := map[string]bool{}
			for _, m := range ms {
				if m.Type != tc.only {
					t.Errorf("%s has type %s, want only %s", m.ID, m.Type, tc.only)
				}
				want, ok := tc.want[m.scope()]
				if !ok {
					continue
				}
				seen[m.scope()] = true
				if m.Status != want {
					t.Errorf("%s: %s, want %s", m.ID, m.Status, want)
				}
			}
			for scope := range tc.want {
				if !seen[scope] {
					t.Errorf("no %s mutant in %s", tc.only, scope)
				}
			}
		})
	}
	ms, _ := mustGamut(t, "-no-cache", dir)
	for _, m := range ms {
		if slices.Contains(mutant.OptIn, mutant.Type(m.Type)) {
			t.Errorf("the default operators enumerated the opt-in %s", m.ID)
		}
	}
}
