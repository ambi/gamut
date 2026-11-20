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
	args := []string{"list", "-e", "-deps", "-test",
		"-json=ImportPath,Dir,GoFiles,CgoFiles,EmbedFiles,TestEmbedFiles,XTestEmbedFiles,Deps,Module"}
	if tags := pkgs[0].Tags; len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, ","))
	}
	for _, p := range pkgs {
		args = append(args, p.ImportPath)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = pkgs[0].ModuleDir
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

	result := map[string][]string{}
	for _, pkg := range pkgs {
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
