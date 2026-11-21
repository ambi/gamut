package mutant_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/mutant"
)

func TestInfos_DescribeEveryKnownOperator(t *testing.T) {
	var names []mutant.Type
	for _, info := range mutant.Infos() {
		names = append(names, info.Type)
		if info.Description == "" || info.Example == "" {
			t.Errorf("%s lacks a description or an example", info.Type)
		}
		if info.Default != slices.Contains(mutant.AllTypes, info.Type) {
			t.Errorf("%s: Default %t disagrees with AllTypes", info.Type, info.Default)
		}
	}
	if !slices.Equal(names, mutant.Known) {
		t.Errorf("Infos lists %v, want Known %v in its order", names, mutant.Known)
	}
}

// TestInfos_MatchTheReadme は、README の変異演算子の説明が、すべての演算子の名前を挙げることを確かめる。
func TestInfos_MatchTheReadme(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range mutant.Infos() {
		if !strings.Contains(string(data), "`"+string(info.Type)+"`") {
			t.Errorf("README does not mention %s", info.Type)
		}
	}
}
