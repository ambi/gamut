package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ambi/gamut/internal/config"
	"github.com/ambi/gamut/internal/mutant"
)

func writeConfig(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), config.FileName)
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_ReadsEveryItem(t *testing.T) {
	path := writeConfig(t, `{
		"mutators": ["ARITHMETIC_BASE", "CONDITIONALS_NEGATION"],
		"exclude_files": ["_gen\\.go$", "^internal/legacy/"],
		"timeout": {"factor": 3, "min": "1500ms"},
		"workers": 3,
		"tags": ["integration"],
		"test_flags": ["-short"],
		"critical": ["auth/**", "billing/fee.go"],
		"thresholds": {"high": 90, "low": 70, "break": 50.5},
		"fail_on_survived": true
	}`)
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"only":             {"ARITHMETIC_BASE,CONDITIONALS_NEGATION"},
		"exclude-files":    {`_gen\.go$`, "^internal/legacy/"},
		"timeout-factor":   {"3"},
		"timeout-min":      {"1.5s"},
		"workers":          {"3"},
		"tags":             {"integration"},
		"test-flags":       {"-short"},
		"critical":         {"auth/**", "billing/fee.go"},
		"threshold-high":   {"90"},
		"threshold-low":    {"70"},
		"threshold-break":  {"50.5"},
		"fail-on-survived": {"true"},
	}
	if got := c.Flags(); !reflect.DeepEqual(got, want) {
		t.Errorf("Flags() = %v, want %v", got, want)
	}
}

func TestLoad_OmittedItemsSetNoFlag(t *testing.T) {
	c, err := config.Load(writeConfig(t, `{"workers": 2}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Flags(); !reflect.DeepEqual(got, map[string][]string{"workers": {"2"}}) {
		t.Errorf("Flags() = %v, want only workers", got)
	}
}

func TestLoad_RejectsInvalidFiles(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"unknown item", `{"mutatorz": []}`, "mutatorz"},
		{"unknown nested item", `{"timeout": {"max": "1s"}}`, "max"},
		{"unknown mutator", `{"mutators": ["NOPE"]}`, "NOPE"},
		{"bad regexp", `{"exclude_files": ["("]}`, "exclude_files"},
		{"bad duration", `{"timeout": {"min": "soon"}}`, "timeout.min"},
		{"non-positive factor", `{"timeout": {"factor": 0}}`, "timeout.factor"},
		{"non-positive workers", `{"workers": 0}`, "workers"},
		{"threshold out of range", `{"thresholds": {"break": 120}}`, "thresholds.break"},
		{"low above high", `{"thresholds": {"high": 60, "low": 80}}`, "thresholds.low"},
		{"unknown threshold", `{"thresholds": {"medium": 1}}`, "medium"},
		{"trailing data", `{"workers": 1} {}`, "after"},
		{"not JSON", `workers: 1`, "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Load(writeConfig(t, tc.src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Load() error = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func TestFind_ReturnsNilWithoutAFile(t *testing.T) {
	c, err := config.Find(t.TempDir())
	if err != nil || c != nil {
		t.Errorf("Find(empty dir) = %v, %v, want nil, nil", c, err)
	}
}

func TestFind_ReadsTheFileAtTheModuleRoot(t *testing.T) {
	path := writeConfig(t, `{"workers": 5}`)
	c, err := config.Find(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.Path != path {
		t.Fatalf("Find() = %+v, want the config at %s", c, path)
	}
}

// TestSchema_MatchesTheConfig は、JSON Schema の項目が、設定の構造体の項目と一致することを確かめる。
func TestSchema_MatchesTheConfig(t *testing.T) {
	data, err := os.ReadFile("../../schema/gamut.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		AdditionalProperties *bool `json:"additionalProperties"`
		Properties           map[string]struct {
			AdditionalProperties *bool                      `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Errorf("the schema allows unknown items")
	}
	keys := func(m map[string]struct {
		AdditionalProperties *bool                      `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
	}) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		return slices.Sorted(slices.Values(out))
	}
	if got, want := keys(schema.Properties), config.Items(); !slices.Equal(got, want) {
		t.Errorf("schema items %v, config items %v", got, want)
	}
	timeout := schema.Properties["timeout"]
	var nested []string
	for k := range timeout.Properties {
		nested = append(nested, k)
	}
	if got := slices.Sorted(slices.Values(nested)); !slices.Equal(got, []string{"factor", "min"}) {
		t.Errorf("schema timeout items %v, want [factor min]", got)
	}
	if timeout.AdditionalProperties == nil || *timeout.AdditionalProperties {
		t.Errorf("the schema allows unknown timeout items")
	}
}

// TestSchema_ListsEveryMutator は、JSON Schema の mutators の候補が、gamut の変異演算子と一致することを確かめる。
func TestSchema_ListsEveryMutator(t *testing.T) {
	data, err := os.ReadFile("../../schema/gamut.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Mutators struct {
				Items struct {
					Enum []string `json:"enum"`
				} `json:"items"`
			} `json:"mutators"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, m := range mutant.Known {
		want = append(want, string(m))
	}
	if got := schema.Properties.Mutators.Items.Enum; !slices.Equal(got, want) {
		t.Errorf("schema mutators %v, want %v", got, want)
	}
}

func TestLoad_DurationsKeepGoSyntax(t *testing.T) {
	c, err := config.Load(writeConfig(t, `{"timeout": {"min": "90s"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Flags()["timeout-min"][0]; got != (90 * time.Second).String() {
		t.Errorf("timeout-min = %q, want %q", got, (90 * time.Second).String())
	}
}
