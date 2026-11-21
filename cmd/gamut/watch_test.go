package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWatch_JudgesTheChangedFunction は、-watch が、保存されたファイルの変更を見つけて、変わった関数の変異体だけを判定することを確かめる。
func TestWatch_JudgesTheChangedFunction(t *testing.T) {
	dir := copyModule(t, "../../testdata/selecttests")
	gitRepo(t, dir)
	reportPath := filepath.Join(t.TempDir(), "r.json")
	runs := make(chan int)
	next := make(chan bool)
	watchHook = func(run int) bool {
		runs <- run
		return <-next
	}
	t.Cleanup(func() { watchHook = nil })
	done := make(chan int)
	go func() {
		done <- realMain([]string{"-watch", "-no-cache", "-o", reportPath, dir}, io.Discard, io.Discard)
	}()
	wait := func(want int) {
		t.Helper()
		select {
		case got := <-runs:
			if got != want {
				t.Fatalf("run %d, want %d", got, want)
			}
		case <-time.After(60 * time.Second):
			t.Fatalf("run %d did not finish", want)
		}
	}
	// 1 回目は、変更がないので何も判定しない。
	wait(1)
	for _, m := range readReport(t, reportPath) {
		if m.Status != "PENDING" {
			t.Errorf("run 1: %s is %s, want PENDING", m.ID, m.Status)
		}
	}
	edit(t, filepath.Join(dir, "calc.go"), "return a * b", "return b * a")
	next <- true
	wait(2)
	judged := 0
	for _, m := range readReport(t, reportPath) {
		inMul := strings.Contains(m.ID, "::Mul::")
		if inMul != (m.Status != "PENDING") {
			t.Errorf("run 2: %s is %s", m.ID, m.Status)
		}
		if inMul {
			judged++
		}
	}
	if judged == 0 {
		t.Error("run 2 judged no mutant of Mul")
	}
	next <- false
	if code := <-done; code != 0 {
		t.Errorf("exit %d", code)
	}
}
