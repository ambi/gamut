package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ambi/gamut/internal/load"
)

const (
	// watchPoll is the interval for comparing modification times and sizes of .go files. The standard library has no file watching, so changes are found by comparison.
	watchPoll = 500 * time.Millisecond
	// watchSettle is the wait between finding a change and judging, so files saved in quick succession make one judgment.
	watchSettle = time.Second
)

// watchHook is a hook for tests only, called after the n-th judgment. Returning false ends the watch.
var watchHook func(n int) bool

// watchLoop judges with args and judges again each time a .go file of the module changes, until SIGINT or SIGTERM.
// Failed judgments (failing baseline tests, the quality gate) do not stop it. Saves during a judgment are combined into the next one.
func watchLoop(args, patterns []string, stdout, stderr io.Writer) int {
	abs, err := load.AbsPatterns(patterns)
	if err != nil || len(abs) == 0 {
		return judge(args, stdout, stderr, modeJudge)
	}
	root := strings.TrimSuffix(abs[0], filepath.FromSlash("/..."))
	if r := load.ModuleRoot(root); r != "" {
		root = r
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for n := 1; ; n++ {
		// Taken before judging, so saves during the judgment are found by the comparison after it.
		before := snapshot(root)
		code := judge(args, stdout, stderr, modeJudge)
		fmt.Fprintf(stderr, "watch: run %d exited %d; waiting for changes to .go files in %s\n", n, code, root)
		if watchHook != nil && !watchHook(n) || ctx.Err() != nil {
			return code
		}
		for maps.Equal(before, snapshot(root)) {
			select {
			case <-ctx.Done():
				return code
			case <-time.After(watchPoll):
			}
		}
		select {
		case <-ctx.Done():
			return code
		case <-time.After(watchSettle):
		}
	}
}

// fileState is a file's modification time and size, for finding changes.
type fileState struct {
	mod  time.Time
	size int64
}

// snapshot returns the state of the .go files under root, skipping directories whose names start with . (.git, .gamut), testdata, and vendor.
func snapshot(root string) map[string]fileState {
	out := map[string]fileState{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		if info, err := d.Info(); err == nil {
			out[path] = fileState{mod: info.ModTime(), size: info.Size()}
		}
		return nil
	})
	return out
}
