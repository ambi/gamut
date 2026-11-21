// Package equiv compares a surviving mutant's compiled code with the original's, and treats it as equivalent when they are the same.
//
// It compares the assembly of go tool compile -S without position notes. The compiler folds constants and removes dead branches,
// so mutants that no input can observe, such as one turning x * 1 into x / 1, are told apart without running tests.
// It calls go tool compile directly rather than go build, so per-mutant compile results never go to the shared build cache.
package equiv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

// ErrUnsupported means go tool compile cannot compile the package directly.
var ErrUnsupported = errors.New("the package has embedded or assembly files, which need more than its Go files to compile")

// Detector compares the compiled code of one package's mutants with the original's. It is safe for concurrent use.
type Detector struct {
	pkg       *load.Package
	work      string
	importcfg string
	base      []byte
}

// New compiles pkg from its original source as the base for comparison. Work files go under work.
func New(ctx context.Context, pkg *load.Package, work string) (*Detector, error) {
	if !pkg.Direct {
		return nil, fmt.Errorf("%s: %w", pkg.ImportPath, ErrUnsupported)
	}
	if err := os.MkdirAll(work, 0o700); err != nil {
		return nil, err
	}
	d := &Detector{pkg: pkg, work: work, importcfg: filepath.Join(work, "importcfg")}
	if err := os.WriteFile(d.importcfg, []byte(pkg.ImportConfig()), 0o600); err != nil {
		return nil, err
	}
	base, err := d.compile(ctx, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", pkg.ImportPath, err)
	}
	d.base = base
	return d, nil
}

// Equivalent reports whether the package compiled with the mutant m matches the original.
// A mutant that does not compile is not equivalent.
func (d *Detector) Equivalent(ctx context.Context, m *mutant.Mutant) (bool, error) {
	original := m.OriginalText
	src := m.File.Src
	if m.Offset+len(original) > len(src) || string(src[m.Offset:m.Offset+len(original)]) != original {
		return false, fmt.Errorf("%s: the source at the mutant's offset is not %q", m.ID, original)
	}
	mutated := make([]byte, 0, len(src)+2)
	mutated = append(append(append(mutated, src[:m.Offset]...), m.ReplacementText...), src[m.Offset+len(original):]...)
	out, err := d.compile(ctx, m.File, mutated)
	if err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return false, nil
		}
		return false, err
	}
	return bytes.Equal(out, d.base), nil
}

// compile writes the package's files to a temporary directory, replaces only file with src, compiles, and returns the normalized assembly.
func (d *Detector) compile(ctx context.Context, file *load.File, src []byte) ([]byte, error) {
	dir, err := os.MkdirTemp(d.work, "c-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	args := []string{"tool", "compile", "-p", d.pkg.ImportPath, "-complete", "-importcfg", d.importcfg,
		"-trimpath", dir, "-o", filepath.Join(dir, "_pkg_.o"), "-S", "-dwarf=false"}
	if lang := langVersion(d.pkg.GoVersion); lang != "" {
		args = append(args, "-lang="+lang)
	}
	for _, f := range append(append([]*load.File{}, d.pkg.Files...), d.pkg.Others...) {
		path := filepath.Join(dir, filepath.Base(f.Path))
		content := f.Src
		if f == file {
			content = src
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			return nil, err
		}
		args = append(args, path)
	}
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = d.pkg.Dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return positions.ReplaceAll(out, nil), nil
}

// positions matches the position notes on assembly instructions ("(file.go:12)"). A mutation that changes the text's length keeps the lines,
// but only the code is compared.
var positions = regexp.MustCompile(`\([^()\n]*\.go:\d+\)`)

// langVersion makes the -lang value ("go1.27") from the go version in go.mod ("1.27.4").
func langVersion(v string) string {
	if v == "" {
		return ""
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return "go" + v
	}
	return "go" + parts[0] + "." + parts[1]
}

// Stats are the counts from Mark.
type Stats struct {
	// Checked, Reused, and Equivalent count the mutants compiled and compared, those using a cached comparison, and those found equivalent.
	Checked, Reused, Equivalent int
	// Skipped are the import paths of packages not compared because they cannot be compiled directly.
	Skipped []string
}

// Mark compares the LIVED mutants in results of targets and makes those whose compiled code matches the original EQUIVALENT.
// With store, comparisons are recorded under the same keys as viability (enclosing declaration, dependencies, external dependencies), and mutants with unchanged keys are not recompiled.
// results[i] are the verdicts of targets[i]. Work files go under work.
func Mark(ctx context.Context, targets []run.Target, results [][]*run.Result, store *cache.Store, workers int, work string) (Stats, error) {
	type job struct {
		target int
		result *run.Result
		key    string
	}
	var stats Stats
	var jobs []job
	for i, t := range targets {
		for _, r := range results[i] {
			if r == nil || r.Status != run.Lived {
				continue
			}
			key := ""
			if t.Keys != nil {
				key = t.Keys.Viability(r.Mutant)
			}
			if store != nil {
				if equal, ok := store.Equivalent(r.Mutant.ID, key); ok {
					stats.Reused++
					if equal {
						markEquivalent(r)
						stats.Equivalent++
					}
					continue
				}
			}
			jobs = append(jobs, job{target: i, result: r, key: key})
		}
	}
	detectors := make([]*Detector, len(targets))
	derrs := make([]error, len(targets))
	var once = make([]sync.Once, len(targets))
	detector := func(i int) (*Detector, error) {
		once[i].Do(func() {
			detectors[i], derrs[i] = New(ctx, targets[i].Pkg, filepath.Join(work, fmt.Sprint(i)))
		})
		return detectors[i], derrs[i]
	}
	equal := make([]bool, len(jobs))
	errs := make([]error, len(jobs))
	skipped := make([]bool, len(targets))
	var mu sync.Mutex
	parallel(len(jobs), workers, func(k int) {
		j := jobs[k]
		d, err := detector(j.target)
		if errors.Is(err, ErrUnsupported) {
			mu.Lock()
			skipped[j.target] = true
			mu.Unlock()
			return
		}
		if err != nil {
			errs[k] = err
			return
		}
		equal[k], errs[k] = d.Equivalent(ctx, j.result.Mutant)
	})
	if err := errors.Join(errs...); err != nil {
		return stats, err
	}
	for i, s := range skipped {
		if s {
			stats.Skipped = append(stats.Skipped, targets[i].Pkg.ImportPath)
		}
	}
	for k, j := range jobs {
		if skipped[j.target] {
			continue
		}
		stats.Checked++
		if store != nil {
			store.PutEquivalent(j.result.Mutant.ID, &cache.Equivalence{Package: targets[j.target].Pkg.ImportPath, Key: j.key, Equal: equal[k]})
		}
		if equal[k] {
			markEquivalent(j.result)
			stats.Equivalent++
		}
	}
	return stats, nil
}

func markEquivalent(r *run.Result) {
	r.Status, r.Reason = run.Equivalent, run.ReasonSameCode
}

func parallel(n, workers int, fn func(int)) {
	ch := make(chan int)
	var wg sync.WaitGroup
	for range max(1, min(workers, n)) {
		wg.Go(func() {
			for i := range ch {
				fn(i)
			}
		})
	}
	for i := range n {
		ch <- i
	}
	close(ch)
	wg.Wait()
}
