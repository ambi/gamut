package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// TestReporter_AgentWritesOneJSONDocument は、-reporter agent が標準出力に JSON を 1 つだけ書き、
// 生き残りごとに分類、位置、変異の前後、囲む宣言、テスト、指針を持つことを確かめる。
func TestReporter_AgentWritesOneJSONDocument(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := realMain([]string{"-no-cache", "-workers", "2", "-reporter", "agent", "../../testdata/fixture"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr.String())
	}
	dec := json.NewDecoder(strings.NewReader(stdout.String()))
	var out struct {
		Survivors []map[string]any `json:"survivors"`
	}
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if dec.More() {
		t.Errorf("stdout holds more than one JSON document")
	}
	if len(out.Survivors) == 0 {
		t.Fatal("no survivors")
	}
	for _, s := range out.Survivors {
		for _, key := range []string{"id", "class", "file", "line", "column", "mutator", "diff", "declaration", "tests", "tests_total", "hint"} {
			if _, ok := s[key]; !ok {
				t.Errorf("survivor %v lacks %q", s["id"], key)
			}
		}
	}
	// 進行状況は標準エラーに出る。
	if !strings.Contains(stderr.String(), "execute:") {
		t.Errorf("want progress on stderr, got:\n%s", stderr.String())
	}
	if code := realMain([]string{"-reporter", "agent", "-reporter", "text", "../../testdata/fixture"}, io.Discard, io.Discard); code != exitError {
		t.Errorf("agent with text: exit %d, want %d", code, exitError)
	}
}
