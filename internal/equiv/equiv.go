// Package equiv は、生き残った変異体のコンパイル結果を元のコンパイル結果と比べ、同じなら等価な変異体とする。
//
// 比べるのは go tool compile -S のアセンブリで、位置の注記を除く。コンパイラは定数の畳み込みと不要な分岐の除去を行うので、
// x * 1 を x / 1 にする変異体のように、どの入力でも観測できない変異体を、テストを実行せずに見分けられる。
// go build ではなく go tool compile を直接呼ぶので、変異体ごとのコンパイル結果を共有のビルドキャッシュへ書かない。
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

// ErrUnsupported は、パッケージを go tool compile で直接コンパイルできないことを表す。
var ErrUnsupported = errors.New("the package has embedded or assembly files, which need more than its Go files to compile")

// Detector は、1 つのパッケージの変異体のコンパイル結果を、元のコンパイル結果と比べる。並行に使える。
type Detector struct {
	pkg       *load.Package
	work      string
	importcfg string
	base      []byte
}

// New は、pkg を元のソースでコンパイルし、比べる元にする。作業ファイルは work の下に置く。
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

// Equivalent は、変異体 m を当てたパッケージのコンパイル結果が、元と同じかを返す。
// 変異体がコンパイルできなければ、等価ではないとする。
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

// compile は、パッケージのファイルを一時ディレクトリに書き、file だけを src に置き換えてコンパイルし、正規化したアセンブリを返す。
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

// positions は、アセンブリの命令に付く位置の注記（"(file.go:12)"）である。変異で字句の長さが変わっても、行は変わらないが、
// 比べるのはコードだけにする。
var positions = regexp.MustCompile(`\([^()\n]*\.go:\d+\)`)

// langVersion は、go.mod の go の版（"1.27.4"）から、-lang の値（"go1.27"）を作る。
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

// Stats は、Mark の結果の数である。
type Stats struct {
	// Checked はコンパイルして比べた変異体、Reused はキャッシュの比較を使った変異体、Equivalent は等価とした変異体の数である。
	Checked, Reused, Equivalent int
	// Skipped は、直接コンパイルできないので比べなかったパッケージの import パスである。
	Skipped []string
}

// Mark は、targets の判定 results のうち LIVED の変異体を比べ、コンパイル結果が元と同じものを EQUIVALENT にする。
// store があれば、構築可能性と同じ鍵（囲む宣言、依存、外の依存）で比較を記録し、鍵の変わらない変異体はコンパイルし直さない。
// results[i] は targets[i] の判定である。作業ファイルは work の下に置く。
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
