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
	// watchPoll は、.go ファイルの更新時刻と大きさを比べる間隔である。標準ライブラリにファイルの監視がないので、比べて見つける。
	watchPoll = 500 * time.Millisecond
	// watchSettle は、変更を見つけてから判定を始めるまでの待ちで、続けて保存されたファイルを 1 回の判定にまとめる。
	watchSettle = time.Second
)

// watchHook はテスト専用の差し替え口で、n 回目の判定が終わるたびに呼ばれる。偽を返すと監視を終える。
var watchHook func(n int) bool

// watchLoop は、args で判定し、モジュールの .go ファイルが変わるたびに判定し直す。SIGINT か SIGTERM で終わる。
// 判定の失敗（基準のテストの失敗、品質の条件）では止まらない。判定の間の保存は、次の 1 回の判定にまとめる。
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
		// 判定の前に取るので、判定の間の保存は、判定の後の比較で見つかる。
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

// fileState は、変更を見つけるための、ファイルの更新時刻と大きさである。
type fileState struct {
	mod  time.Time
	size int64
}

// snapshot は、root の下の .go ファイルの状態を返す。名前が . で始まるディレクトリ（.git、.gamut）と testdata、vendor は見ない。
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
