package run

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/schemata"
)

// server は、変異体を順に判定するテストプロセスである。gamut の 1 回の実行の間だけ動く。
// パッケージの初期化（テストの変数の初期化を含む）を変異体ごとに繰り返さないために使う。
type server struct {
	cmd   *cmdHandle
	in    io.WriteCloser
	lines chan string
}

type cmdHandle struct {
	stop func()
}

// pool は、テストバイナリごとの待機中の server である。
type pool struct {
	mu   sync.Mutex
	idle map[*binary][]*server
	ctx  context.Context
	opts Options
}

func newPool(ctx context.Context, opts Options) *pool {
	return &pool{idle: map[*binary][]*server{}, ctx: ctx, opts: opts}
}

// get は、b の待機中の server を返す。なければ起動する。
func (p *pool) get(b *binary) (*server, error) {
	p.mu.Lock()
	if list := p.idle[b]; len(list) > 0 {
		s := list[len(list)-1]
		p.idle[b] = list[:len(list)-1]
		p.mu.Unlock()
		return s, nil
	}
	p.mu.Unlock()
	return startServer(p.ctx, b, p.opts, nil)
}

func (p *pool) put(b *binary, s *server) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.idle[b] = append(p.idle[b], s)
}

func (p *pool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, list := range p.idle {
		for _, s := range list {
			s.close()
		}
	}
	p.idle = map[*binary][]*server{}
}

// startServer は、b の判定のテストを起動する。env は足す環境変数である。
func startServer(ctx context.Context, b *binary, opts Options, env []string) (*server, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	cmd := testCommand(runCtx, b.dir, b.path, opts.TestFlags, append([]string{"GAMUT_SERVE=1", "GAMUT_MUTANT=0"}, env...),
		"-test.run", "^"+schemata.ServerTestName+"$", "-test.count=1", "-test.timeout=0", "-test.paniconexit0")
	cmd.ExtraFiles = []*os.File{w}
	// 失敗したテストの出力は使わないので捨てる。
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	_ = w.Close()
	s := &server{in: in, lines: make(chan string, 16)}
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		close(s.lines)
		_ = r.Close()
	}()
	var once sync.Once
	s.cmd = &cmdHandle{stop: func() {
		once.Do(func() {
			_ = in.Close()
			cancel()
			_ = cmd.Wait()
		})
	}}
	return s, nil
}

func (s *server) close() { s.cmd.stop() }

type served int

const (
	servedPass served = iota
	servedFail
	servedDied
	servedTimeout
)

// run は、変異体 index を有効にして names を順に実行する。最初に失敗したテストの名前を返す。
// プロセスが落ちたら（panic、os.Exit）、そのとき実行していたテストが検出したとする。
func (s *server) run(ctx context.Context, index int, names []string, limit time.Duration) (served, string) {
	outcome, name, _ := s.do(ctx, strconv.Itoa(index), names, limit)
	return outcome, name
}

// cover は、names を変異なしで順に実行して被覆を記録させ、各テストの所要時間を返す。終わると、プロセスは被覆の記録を止める。
func (s *server) cover(ctx context.Context, names []string) (served, map[string]time.Duration) {
	outcome, _, durations := s.do(ctx, "cover", names, 0)
	return outcome, durations
}

// do は、命令 head と names を送り、結果を待つ。limit が 0 なら時間を区切らない。
func (s *server) do(ctx context.Context, head string, names []string, limit time.Duration) (served, string, map[string]time.Duration) {
	if _, err := fmt.Fprintf(s.in, "%s %s\n", head, strings.Join(names, " ")); err != nil {
		return servedDied, "", nil
	}
	var expired <-chan time.Time
	if limit > 0 {
		timer := time.NewTimer(limit)
		defer timer.Stop()
		expired = timer.C
	}
	current := ""
	durations := map[string]time.Duration{}
	for {
		select {
		case line, ok := <-s.lines:
			switch {
			case !ok:
				return servedDied, current, durations
			case strings.HasPrefix(line, "run "):
				current = strings.TrimPrefix(line, "run ")
			case strings.HasPrefix(line, "d "):
				if name, ns, ok := strings.Cut(strings.TrimPrefix(line, "d "), " "); ok {
					if n, err := strconv.ParseInt(ns, 10, 64); err == nil {
						durations[name] = time.Duration(n)
					}
				}
			case line == "pass":
				return servedPass, "", durations
			case strings.HasPrefix(line, "fail "):
				return servedFail, strings.TrimPrefix(line, "fail "), durations
			}
		case <-expired:
			return servedTimeout, current, durations
		case <-ctx.Done():
			return servedTimeout, current, durations
		}
	}
}

// canServe は、tests をすべて判定のテストから呼べるかを返す。
func (u *unit) canServe(tests []*testCase) bool {
	if u.served == nil {
		return false
	}
	for _, t := range tests {
		if t.bin != u.bins[0] || !u.served[t.local] {
			return false
		}
	}
	return true
}

// serve は、変異体 m を、待機中のテストプロセスで判定する。テストは前回の検出テストを先に、残りを短い順に実行する。
// 検出したら、そのテストを変異なしでもう一度実行し、通らなければ、変異体がプロセスの状態を汚したとみなしてプロセスを捨てる。
// 落ちたか時間切れになったプロセスも捨てる。
func (u *unit) serve(ctx context.Context, m *mutant.Mutant, tests []*testCase, opts Options, p *pool) *Result {
	var order []*testCase
	for _, g := range phases(tests, u.priorKillers(m, opts.Cache), u.bins) {
		order = append(order, g...)
	}
	names := testNames(order)
	if opts.onExecute != nil {
		opts.onExecute(m, [][]string{names})
	}
	locals := make([]string, len(order))
	for i, t := range order {
		locals[i] = t.local
	}
	start := time.Now()
	r := &Result{Mutant: m}
	s, err := p.get(u.bins[0])
	if err != nil {
		return u.execute(ctx, m, tests, opts, func(b *binary) (string, error) { return b.path, nil })
	}
	limit := time.Duration(float64(total(order))*opts.TimeoutFactor) + opts.TimeoutMin
	outcome, name := s.run(ctx, m.Index, locals, limit)
	r.Elapsed = time.Since(start)
	switch outcome {
	case servedPass:
		r.Status, r.Reason = Lived, ReasonNotDetected
		p.put(u.bins[0], s)
	case servedFail, servedDied:
		r.Status, r.KilledBy, r.Killer = Killed, name, name
		if outcome == servedDied {
			s.close()
			break
		}
		// 状態の汚れの確認：検出したテストは、変異なしなら通るはずである。
		// 通らなければ、前の変異体か今の変異体がプロセスの状態を汚した。今の判定も汚れた状態から出たかもしれないので、
		// プロセスを捨て、新しいプロセスで判定し直す。
		if check, _ := s.run(ctx, 0, []string{name}, time.Duration(float64(limit)*2)); check != servedPass {
			s.close()
			return u.execute(ctx, m, tests, opts, func(b *binary) (string, error) { return b.path, nil })
		}
		p.put(u.bins[0], s)
	case servedTimeout:
		r.Status = TimedOut
		s.close()
	}
	return r
}
