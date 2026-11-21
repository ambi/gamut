package main

import (
	"maps"
	"testing"
)

// TestOperators_OptInVerdicts は、既定では無効な変異演算子を -only で選ぶと列挙され、固定モジュールで期待どおりに判定されることを確かめる。
// 既定の集合では列挙されないことも確かめる。
func TestOperators_OptInVerdicts(t *testing.T) {
	dir := "../../testdata/operators"
	cases := []struct {
		only string
		want map[string]string // 囲む宣言 -> 状態
	}{
		{"INVERT_BITWISE", map[string]string{"Flags": "KILLED", "Mask": "LIVED", "Double": "KILLED"}},
	}
	for _, tc := range cases {
		t.Run(tc.only, func(t *testing.T) {
			ms, _ := mustGamut(t, "-no-cache", "-only", tc.only, dir)
			got := map[string]string{}
			for _, m := range ms {
				if m.Type != tc.only {
					t.Errorf("%s has type %s, want only %s", m.ID, m.Type, tc.only)
				}
				got[m.scope()] = m.Status
			}
			if !maps.Equal(got, tc.want) {
				t.Errorf("verdicts %v, want %v", got, tc.want)
			}
		})
	}
	ms, _ := mustGamut(t, "-no-cache", dir)
	if len(ms) != 0 {
		t.Errorf("the default operators found %d mutants in a module with only opt-in sites", len(ms))
	}
}
