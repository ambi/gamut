package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDetectEquivalent_MarksSurvivorsWithTheSameMachineCode は、-detect-equivalent が、コンパイル結果が元と同じ生き残りだけを
// EQUIVALENT にし、数から外し、次の実行ではキャッシュの比較を使うことを確かめる。
func TestDetectEquivalent_MarksSurvivorsWithTheSameMachineCode(t *testing.T) {
	dir := "../../testdata/equiv"
	plain, _ := mustGamut(t, "-no-cache", "-only", "ARITHMETIC_BASE", dir)
	for _, m := range plain {
		if m.Status != "LIVED" {
			t.Fatalf("%s is %s without -detect-equivalent, want every mutant LIVED", m.ID, m.Status)
		}
	}
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	dir = copyModule(t, dir)
	for i, wantLog := range []string{"2 survivors compiled, 0 reused from the cache, 1 equivalent", "0 survivors compiled, 2 reused from the cache, 1 equivalent"} {
		if i > 0 {
			// 中身を変えずに更新時刻だけを変え、変更のない再実行の近道を外す。比較の鍵は中身から作るので変わらない。
			later := time.Now().Add(time.Minute)
			if err := os.Chtimes(filepath.Join(dir, "equiv_test.go"), later, later); err != nil {
				t.Fatal(err)
			}
		}
		ms, log := mustGamut(t, "-cache", cachePath, "-only", "ARITHMETIC_BASE", "-detect-equivalent", dir)
		for _, m := range ms {
			want := "LIVED"
			if m.scope() == "Scale" {
				want = "EQUIVALENT"
			}
			if m.Status != want {
				t.Errorf("run %d: %s is %s, want %s", i+1, m.ID, m.Status, want)
			}
		}
		if !strings.Contains(log, wantLog) {
			t.Errorf("run %d: want %q in the log:\n%s", i+1, wantLog, log)
		}
	}
}
