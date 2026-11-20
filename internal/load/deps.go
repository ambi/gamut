package load

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type depPackage struct {
	ImportPath      string
	Dir             string
	GoFiles         []string
	TestGoFiles     []string
	XTestGoFiles    []string
	CgoFiles        []string
	EmbedFiles      []string
	TestEmbedFiles  []string
	XTestEmbedFiles []string
	Deps            []string
	Module          *struct {
		Main    bool
		Replace *struct{ Version string }
	}
}

// local は、パッケージのソースが利用者の手元で変わりうるか、つまりメインモジュール（ワークスペースを含む）か、
// 版を持たないディレクトリへの replace の先にあるかを返す。
// それ以外のモジュールの中身は go.mod と go.sum が決め、標準ライブラリは Go の版が決める。
func (p *depPackage) local() bool {
	return p.Module != nil && (p.Module.Main || p.Module.Replace != nil && p.Module.Replace.Version == "")
}

// SourceDeps は、対象パッケージごとに、その判定に影響しうるファイルのうち、パッケージ自身の Go ファイルとテストファイルを除いたものの絶対パスを返す。
// 中身は、テストバイナリが依存する手元のモジュールのパッケージの Go ファイルと埋め込みファイル、対象自身の埋め込みファイル、対象の testdata ディレクトリのファイルである。
// テストだけが import するパッケージも含めるため、go list -deps -test を実行する。エクスポートデータは作らない。
func SourceDeps(pkgs []*Package) (map[string][]string, error) {
	if len(pkgs) == 0 {
		return map[string][]string{}, nil
	}
	patterns := make([]string, len(pkgs))
	for i, p := range pkgs {
		patterns[i] = p.ImportPath
	}
	deps, err := ListDeps(pkgs[0].ModuleDir, pkgs[0].Tags, patterns)
	if err != nil {
		return nil, err
	}
	return deps.Sources(pkgs)
}

// Deps は、go list -deps -test の結果である。
type Deps struct {
	listed map[string]*depPackage
}

// ListDeps は、モジュールのディレクトリ dir で、patterns に go list -deps -test を実行する。
// Package を取らないので、Load と並行して実行できる。patterns は Load に渡すものと同じパッケージを指せばよい。
func ListDeps(dir string, tags, patterns []string) (*Deps, error) {
	listed, err := listTestDeps(dir, tags, patterns)
	if err != nil {
		return nil, err
	}
	return &Deps{listed: listed}, nil
}

// Sources は、SourceDeps と同じものを、一覧 d から求める。d は pkgs をすべて含めて列挙したものでなければならない。
func (d *Deps) Sources(pkgs []*Package) (map[string][]string, error) {
	listed := d.listed
	result := map[string][]string{}
	for _, pkg := range pkgs {
		if listed[pkg.ImportPath] == nil {
			return nil, fmt.Errorf("%s is not in the dependency list", pkg.ImportPath)
		}
		files := map[string]bool{}
		add := func(dir string, names ...[]string) {
			for _, name := range slices.Concat(names...) {
				files[filepath.Join(dir, name)] = true
			}
		}
		if self := listed[pkg.ImportPath]; self != nil {
			add(self.Dir, self.EmbedFiles, self.TestEmbedFiles, self.XTestEmbedFiles)
		}
		// テストバイナリの依存は p.test が持つ。テストファイルがなければ p 自身の依存を使う。
		root := listed[pkg.ImportPath+".test"]
		if root == nil {
			root = listed[pkg.ImportPath]
		}
		if root != nil {
			for _, d := range root.Deps {
				// テスト用に再コンパイルされた依存は "q [p.test]" と書かれる。ファイルは q と同じである。
				base, _, _ := strings.Cut(d, " ")
				if base == pkg.ImportPath || base == pkg.ImportPath+"_test" {
					continue
				}
				if dep := listed[base]; dep != nil && dep.local() {
					add(dep.Dir, dep.GoFiles, dep.CgoFiles, dep.EmbedFiles)
				}
			}
		}
		testdata, err := regularFiles(filepath.Join(pkg.Dir, "testdata"))
		if err != nil {
			return nil, err
		}
		for _, f := range testdata {
			files[f] = true
		}
		result[pkg.ImportPath] = slices.Sorted(maps.Keys(files))
	}
	return result, nil
}

// listTestDeps は、dir で go list -deps -test を patterns に実行し、import パスからパッケージを引く表を返す。
func listTestDeps(dir string, tags, patterns []string) (map[string]*depPackage, error) {
	args := []string{"list", "-e", "-deps", "-test",
		"-json=ImportPath,Dir,GoFiles,CgoFiles,TestGoFiles,XTestGoFiles,EmbedFiles,TestEmbedFiles,XTestEmbedFiles,Deps,Module"}
	if len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, ","))
	}
	cmd := exec.Command("go", append(args, patterns...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -test in %s: %w: %s", cmd.Dir, err, strings.TrimSpace(stderr.String()))
	}
	listed := map[string]*depPackage{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p depPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		listed[p.ImportPath] = &p
	}
	return listed, nil
}

// RevDep は、対象パッケージを推移的に import する、モジュール内のテストを持つパッケージである。
type RevDep struct {
	ImportPath string
	// Dir はパッケージディレクトリの絶対パスである。
	Dir string
	// TestFiles は、テストファイル（外部テストを含む）の絶対パスである。
	TestFiles []string
	// Files は、テストの鍵に含めるファイルの絶対パスである。
	// パッケージ自身の Go ファイル、テストファイル、埋め込みファイル、testdata の下のファイルと、
	// テストバイナリが依存する手元のパッケージ（対象パッケージを除く）の Go ファイルと埋め込みファイルである。
	Files []string
}

// ReverseDeps は、対象パッケージごとに、そのテストバイナリにリンクされるモジュール内のパッケージのうちテストを持つものを返す。
// go list -deps -test をメインモジュールの全パッケージに実行し、テストバイナリの依存を逆にたどる。
// 対象パッケージ自身は含めない。返すパッケージは import パスの順に並ぶ。
func ReverseDeps(pkgs []*Package) (map[string][]RevDep, error) {
	result := map[string][]RevDep{}
	if len(pkgs) == 0 {
		return result, nil
	}
	listed, err := listTestDeps(pkgs[0].ModuleDir, pkgs[0].Tags, []string{"./..."})
	if err != nil {
		return nil, err
	}
	targets := map[string]bool{}
	for _, p := range pkgs {
		targets[p.ImportPath] = true
	}
	for name, test := range listed {
		q, ok := strings.CutSuffix(name, ".test")
		self := listed[q]
		if !ok || self == nil || self.Module == nil || !self.Module.Main || len(self.TestGoFiles)+len(self.XTestGoFiles) == 0 {
			continue
		}
		deps := map[string]bool{}
		for _, d := range test.Deps {
			base, _, _ := strings.Cut(d, " ")
			deps[base] = true
		}
		for target := range targets {
			if target == q || !deps[target] {
				continue
			}
			r := RevDep{ImportPath: q, Dir: self.Dir}
			for _, n := range slices.Concat(self.TestGoFiles, self.XTestGoFiles) {
				r.TestFiles = append(r.TestFiles, filepath.Join(self.Dir, n))
			}
			files := map[string]bool{}
			add := func(dir string, names ...[]string) {
				for _, n := range slices.Concat(names...) {
					files[filepath.Join(dir, n)] = true
				}
			}
			add(self.Dir, self.GoFiles, self.CgoFiles, self.TestGoFiles, self.XTestGoFiles, self.EmbedFiles, self.TestEmbedFiles, self.XTestEmbedFiles)
			for d := range deps {
				if dep := listed[d]; d != target && d != q && d != q+"_test" && dep != nil && dep.local() {
					add(dep.Dir, dep.GoFiles, dep.CgoFiles, dep.EmbedFiles)
				}
			}
			testdata, err := regularFiles(filepath.Join(self.Dir, "testdata"))
			if err != nil {
				return nil, err
			}
			for _, f := range testdata {
				files[f] = true
			}
			r.Files = slices.Sorted(maps.Keys(files))
			result[target] = append(result[target], r)
		}
	}
	for _, rs := range result {
		slices.SortFunc(rs, func(a, b RevDep) int { return strings.Compare(a.ImportPath, b.ImportPath) })
	}
	return result, nil
}

// regularFiles は、dir の下の通常のファイルを返す。dir がなければ何も返さない。
func regularFiles(dir string) ([]string, error) {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}
