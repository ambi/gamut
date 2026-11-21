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

// server is a test process that judges mutants one after another. It lives only during one gamut run.
// It avoids repeating package initialization (including the tests' variable initialization) per mutant.
type server struct {
	cmd   *cmdHandle
	in    io.WriteCloser
	lines chan string
}

type cmdHandle struct {
	stop func()
}

// pool holds the idle servers per test binary.
type pool struct {
	mu   sync.Mutex
	idle map[*binary][]*server
	ctx  context.Context
	opts Options
}

func newPool(ctx context.Context, opts Options) *pool {
	return &pool{idle: map[*binary][]*server{}, ctx: ctx, opts: opts}
}

// get returns an idle server of b, starting one when there is none.
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

// startServer starts b's judging test. env are additional environment variables.
func startServer(ctx context.Context, b *binary, opts Options, env []string) (*server, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	cmd := testCommand(runCtx, b.dir, b.path, opts.TestFlags, append([]string{"GAMUT_SERVE=1", "GAMUT_MUTANT=0"}, env...),
		"-test.run", "^"+schemata.ServerTestName+"$", "-test.count=1", "-test.timeout=0", "-test.paniconexit0")
	cmd.ExtraFiles = []*os.File{w}
	// The output of failing tests is not used, so it is discarded.
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

// run enables the mutant index and runs names in order. It returns the name of the first failing test.
// If the process dies (panic, os.Exit), the test running at that moment counts as the detector.
func (s *server) run(ctx context.Context, index int, names []string, limit time.Duration) (served, string) {
	outcome, name, _, _ := s.do(ctx, strconv.Itoa(index), names, limit)
	return outcome, name
}

// cover runs names in order without mutations to record coverage, and returns each test's duration and the names of the tests that failed on their second run in the same process.
// When it finishes, the process stops recording coverage.
func (s *server) cover(ctx context.Context, names []string) (served, map[string]time.Duration, []string) {
	outcome, _, durations, unrepeatable := s.do(ctx, "cover", names, 0)
	return outcome, durations, unrepeatable
}

// do sends the command head with names and waits for the result. A limit of 0 means no time limit.
func (s *server) do(ctx context.Context, head string, names []string, limit time.Duration) (outcome served, failed string, durations map[string]time.Duration, unrepeatable []string) {
	if _, err := fmt.Fprintf(s.in, "%s %s\n", head, strings.Join(names, " ")); err != nil {
		return servedDied, "", nil, nil
	}
	var expired <-chan time.Time
	if limit > 0 {
		timer := time.NewTimer(limit)
		defer timer.Stop()
		expired = timer.C
	}
	current := ""
	durations = map[string]time.Duration{}
	for {
		select {
		case line, ok := <-s.lines:
			switch {
			case !ok:
				return servedDied, current, durations, unrepeatable
			case strings.HasPrefix(line, "run "):
				current = strings.TrimPrefix(line, "run ")
			case strings.HasPrefix(line, "d "):
				if name, ns, ok := strings.Cut(strings.TrimPrefix(line, "d "), " "); ok {
					if n, err := strconv.ParseInt(ns, 10, 64); err == nil {
						durations[name] = time.Duration(n)
					}
				}
			case strings.HasPrefix(line, "u "):
				unrepeatable = append(unrepeatable, strings.TrimPrefix(line, "u "))
			case line == "pass":
				return servedPass, "", durations, unrepeatable
			case strings.HasPrefix(line, "fail "):
				return servedFail, strings.TrimPrefix(line, "fail "), durations, unrepeatable
			}
		case <-expired:
			return servedTimeout, current, durations, unrepeatable
		case <-ctx.Done():
			return servedTimeout, current, durations, unrepeatable
		}
	}
}

// canServe reports whether the judging test can call every test in tests.
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

// serve judges the mutant m in an idle test process. It runs the previous detecting test first and the rest shortest first.
// On detection, it runs that test again without mutations; if it does not pass, the mutant dirtied the process's state and the process is thrown away.
// Processes that died or timed out are thrown away too.
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
		// Checking for dirty state: the detecting test should pass without mutations.
		// If it does not, the previous or the current mutant dirtied the process's state. The current verdict may also come from that dirty state,
		// so throw the process away and judge again in a new one.
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
