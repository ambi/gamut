package report_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/report"
)

// schemaFile は、mutation-testing-report-schema 3.9.0 の JSON Schema である（Apache License 2.0、同じディレクトリの LICENSE を参照）。
const schemaFile = "testdata/mutation-testing-report-schema.json"

func TestMTE_ValidatesAgainstTheSchema(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nfunc F(a, b int) int {\n\treturn a + b\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	m := func(id, status, reason string) report.Mutation {
		return report.Mutation{ID: id, Type: "ARITHMETIC_BASE", Status: status, Reason: reason, Line: 4, Column: 11, Original: "+", Replacement: "-"}
	}
	killed := m("p.go::ARITHMETIC_BASE::F::01", "KILLED", "")
	killed.KilledBy, killed.Tests = "TestF", []string{"TestF", "TestG"}
	accepted := m("p.go::ARITHMETIC_BASE::F::06", "LIVED", "not detected")
	accepted.Accepted = "callers read only the sign"
	rep := &report.Report{
		Thresholds: &report.Thresholds{High: 80.4, Low: 59.6},
		Files: []report.File{{FileName: "p.go", Path: "p.go", Mutations: []report.Mutation{
			killed,
			m("p.go::ARITHMETIC_BASE::F::02", "LIVED", "not detected"),
			m("p.go::ARITHMETIC_BASE::F::03", "NOT COVERED", ""),
			m("p.go::ARITHMETIC_BASE::F::04", "NOT VIABLE", ""),
			m("p.go::ARITHMETIC_BASE::F::05", "TIMED OUT", ""),
			accepted,
			m("p.go::ARITHMETIC_BASE::F::07", "EQUIVALENT", "same machine code"),
			m("p.go::ARITHMETIC_BASE::F::08", "PENDING", ""),
		}}},
		Ignored: []report.IgnoredMutation{{ID: "p.go::ARITHMETIC_BASE::F::09", Type: "ARITHMETIC_BASE", Path: "p.go", Line: 4, Column: 11, Original: "+", Replacement: "-", Reason: "a cache key"}},
	}
	out, err := rep.MTE(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	schemaData, err := os.ReadFile(schemaFile)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatal(err)
	}
	if errs := validate(schema, schema, doc, ""); len(errs) > 0 {
		t.Fatalf("the report does not match the schema:\n%s\n%s", strings.Join(errs, "\n"), data)
	}

	file := out.Files["p.go"]
	if file == nil || file.Source != src || file.Language != "go" {
		t.Fatalf("file entry %+v, want the source and language go", file)
	}
	got := map[string]string{}
	for _, mu := range file.Mutants {
		got[mu.ID[len(mu.ID)-2:]] = mu.Status + " " + mu.StatusReason
		if mu.Location.Start.Line != 4 || mu.Location.Start.Column != 11 || mu.Location.End.Column != 12 {
			t.Errorf("%s: location %+v, want 4:11-4:12", mu.ID, mu.Location)
		}
	}
	want := map[string]string{
		"01": "Killed ", "02": "Survived not detected", "03": "NoCoverage ", "04": "CompileError ", "05": "Timeout ",
		"06": "Ignored accepted: callers read only the sign", "07": "Ignored equivalent: same machine code", "08": "Pending ",
		"09": "Ignored ignored by comment: a cache key",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("mutant %s: %q, want %q", k, got[k], w)
		}
	}
	if out.Thresholds.High != 80 || out.Thresholds.Low != 60 {
		t.Errorf("thresholds %+v, want rounded 80 and 60", out.Thresholds)
	}
}

// TestValidate_RejectsBrokenReports は、検証器そのものが、スキーマに合わない報告を拒むことを確かめる。
func TestValidate_RejectsBrokenReports(t *testing.T) {
	schemaData, err := os.ReadFile(schemaFile)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{
		"an unknown status":    `{"schemaVersion":"2","thresholds":{"high":80,"low":60},"files":{"a.go":{"language":"go","source":"","mutants":[{"id":"1","mutatorName":"X","location":{"start":{"line":1,"column":1},"end":{"line":1,"column":2}},"status":"Bogus"}]}}}`,
		"a missing location":   `{"schemaVersion":"2","thresholds":{"high":80,"low":60},"files":{"a.go":{"language":"go","source":"","mutants":[{"id":"1","mutatorName":"X","status":"Killed"}]}}}`,
		"a fractional low":     `{"schemaVersion":"2","thresholds":{"high":80,"low":60.5},"files":{}}`,
		"a bad schema version": `{"schemaVersion":"9","thresholds":{"high":80,"low":60},"files":{}}`,
		"a column of zero":     `{"schemaVersion":"2","thresholds":{"high":80,"low":60},"files":{"a.go":{"language":"go","source":"","mutants":[{"id":"1","mutatorName":"X","location":{"start":{"line":1,"column":0},"end":{"line":1,"column":2}},"status":"Killed"}]}}}`,
	} {
		var v any
		if err := json.Unmarshal([]byte(doc), &v); err != nil {
			t.Fatal(err)
		}
		if errs := validate(schema, schema, v, ""); len(errs) == 0 {
			t.Errorf("%s: the validator accepted the report", name)
		}
	}
}

func TestWriteHTML_EmbedsTheReport(t *testing.T) {
	out := &report.MTEReport{SchemaVersion: "2", Files: map[string]*report.MTEFile{"a.go": {Language: "go", Source: "</script><b>"}}}
	var b strings.Builder
	if err := report.WriteHTML(&b, out); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	if !strings.Contains(html, `<script src="https://cdn.jsdelivr.net/npm/mutation-testing-elements@3.9.0/dist/mutation-test-elements.js"></script>`) {
		t.Errorf("the viewer script is not pinned:\n%s", html)
	}
	// ソースの "</script>" で埋め込みの script が閉じないよう、JSON の < はエスケープする。
	if strings.Count(html, "</script>") != 2 {
		t.Errorf("the embedded source closes a script element:\n%s", html)
	}
}

// validate は、doc を JSON Schema（draft-07）の schema で検証する。このスキーマが使うキーワード
// （type、required、properties、additionalProperties、items、enum、$ref、minimum、maximum、uniqueItems、pattern）だけを扱う。
func validate(root, schema map[string]any, doc any, at string) []string {
	if ref, ok := schema["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/definitions/")
		return validate(root, root["definitions"].(map[string]any)[name].(map[string]any), doc, at)
	}
	var errs []string
	fail := func(format string, args ...any) { errs = append(errs, at+": "+fmt.Sprintf(format, args...)) }
	if typ, ok := schema["type"].(string); ok && !hasType(doc, typ) {
		fail("want %s, got %T", typ, doc)
		return errs
	}
	if enum, ok := schema["enum"].([]any); ok && !slices.Contains(enum, doc) {
		fail("%v is not one of %v", doc, enum)
	}
	if n, ok := doc.(float64); ok {
		if lo, ok := schema["minimum"].(float64); ok && n < lo {
			fail("%v < minimum %v", n, lo)
		}
		if hi, ok := schema["maximum"].(float64); ok && n > hi {
			fail("%v > maximum %v", n, hi)
		}
	}
	if s, ok := doc.(string); ok {
		if p, ok := schema["pattern"].(string); ok && !regexp.MustCompile(p).MatchString(s) {
			fail("%q does not match %s", s, p)
		}
	}
	if obj, ok := doc.(map[string]any); ok {
		for _, r := range anySlice(schema["required"]) {
			if _, ok := obj[r.(string)]; !ok {
				fail("missing required %q", r)
			}
		}
		props, _ := schema["properties"].(map[string]any)
		for k, v := range obj {
			if p, ok := props[k].(map[string]any); ok {
				errs = append(errs, validate(root, p, v, at+"."+k)...)
				continue
			}
			switch extra := schema["additionalProperties"].(type) {
			case bool:
				if !extra {
					fail("unknown property %q", k)
				}
			case map[string]any:
				errs = append(errs, validate(root, extra, v, at+"."+k)...)
			}
		}
	}
	if arr, ok := doc.([]any); ok {
		if items, ok := schema["items"].(map[string]any); ok {
			for i, v := range arr {
				errs = append(errs, validate(root, items, v, fmt.Sprintf("%s[%d]", at, i))...)
			}
		}
		if unique, _ := schema["uniqueItems"].(bool); unique {
			seen := map[string]bool{}
			for _, v := range arr {
				key, _ := json.Marshal(v)
				if seen[string(key)] {
					fail("duplicate item %s", key)
				}
				seen[string(key)] = true
			}
		}
	}
	return errs
}

func hasType(doc any, typ string) bool {
	switch typ {
	case "object":
		_, ok := doc.(map[string]any)
		return ok
	case "array":
		_, ok := doc.([]any)
		return ok
	case "string":
		_, ok := doc.(string)
		return ok
	case "boolean":
		_, ok := doc.(bool)
		return ok
	case "number":
		_, ok := doc.(float64)
		return ok
	case "integer":
		n, ok := doc.(float64)
		return ok && n == math.Trunc(n)
	}
	return false
}

func anySlice(v any) []any {
	s, _ := v.([]any)
	return s
}
