// Package load は、対象パッケージを go list で見つけ、構文木と型情報を作る。
package load

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// HelperImports は、schemata の補助ファイルが import する標準パッケージである。
var HelperImports = []string{"os", "strconv", "sync", "sync/atomic"}

// Package は、変異の対象とする 1 つのパッケージである。
type Package struct {
	// Dir はパッケージディレクトリの絶対パスである。
	Dir string
	// ImportPath はパッケージの import パスである。
	ImportPath string
	// Name はパッケージ名である。
	Name string
	// ModuleDir はパッケージを含むモジュールのルートの絶対パスである。
	ModuleDir string
	// ModulePath はモジュールパスである。
	ModulePath string
	// Fset はすべての構文木の位置を持つ。
	Fset *token.FileSet
	// Files は変異の対象とするファイル（テストと生成ファイルを除く）である。
	Files []*File
	// Others は対象外だが型検査に要る、同じパッケージの残りのファイルである。
	Others []*File
	// Types と Info は Files と Others をまとめて検査した結果である。
	Types *types.Package
	Info  *types.Info

	importer types.Importer
}

// File はパッケージの 1 ファイルである。
type File struct {
	// Path はファイルの絶対パスである。
	Path string
	// Rel はモジュールのルートからの相対パス（区切りは /）である。
	Rel string
	// Src はファイルの内容である。
	Src []byte
	// AST はファイルの構文木である。
	AST *ast.File
}

type listedPackage struct {
	Dir        string
	ImportPath string
	Name       string
	Export     string
	GoFiles    []string
	CgoFiles   []string
	ImportMap  map[string]string
	Module     *struct {
		Path string
		Dir  string
	}
	Error *struct{ Err string }
}

// Load は dir のパッケージを読み、型を検査する。
func Load(dir string) (*Package, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	// schemata の補助ファイルが import するパッケージも、型検査のために一緒に列挙する。
	args := append([]string{"list", "-e", "-json", "-export", "-deps", "."}, HelperImports...)
	cmd := exec.Command("go", args...)
	cmd.Dir = abs
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list in %s: %w: %s", abs, err, strings.TrimSpace(stderr.String()))
	}
	exports := map[string]string{}
	var target *listedPackage
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listedPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		if p.Error != nil {
			return nil, fmt.Errorf("go list %s: %s", p.ImportPath, p.Error.Err)
		}
		exports[p.ImportPath] = p.Export
		if p.Dir == abs {
			pp := p
			target = &pp
		}
	}
	if target == nil {
		return nil, fmt.Errorf("go list did not report the package in %s", abs)
	}
	if len(target.CgoFiles) > 0 {
		return nil, fmt.Errorf("package %s uses cgo, which gamut does not support", target.ImportPath)
	}
	if target.Module == nil {
		return nil, fmt.Errorf("package %s is not in a module", target.ImportPath)
	}

	pkg := &Package{
		Dir:        abs,
		ImportPath: target.ImportPath,
		Name:       target.Name,
		ModuleDir:  target.Module.Dir,
		ModulePath: target.Module.Path,
		Fset:       token.NewFileSet(),
	}
	for _, name := range target.GoFiles {
		path := filepath.Join(abs, name)
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(pkg.Fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(pkg.ModuleDir, path)
		if err != nil {
			return nil, err
		}
		file := &File{Path: path, Rel: filepath.ToSlash(rel), Src: src, AST: f}
		if ast.IsGenerated(f) {
			pkg.Others = append(pkg.Others, file)
		} else {
			pkg.Files = append(pkg.Files, file)
		}
	}

	importMap := target.ImportMap
	pkg.importer = importer.ForCompiler(pkg.Fset, "gc", func(path string) (io.ReadCloser, error) {
		if mapped, ok := importMap[path]; ok {
			path = mapped
		}
		export, ok := exports[path]
		if !ok || export == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(export)
	})

	info := NewInfo()
	tpkg, errs := pkg.Check(pkg.Fset, pkg.AllASTs(), info)
	if len(errs) > 0 {
		return nil, fmt.Errorf("type-check %s: %w", pkg.ImportPath, errs[0])
	}
	pkg.Types = tpkg
	pkg.Info = info
	return pkg, nil
}

// NewInfo は、列挙と書き換えが参照する型情報の入れ物を作る。
func NewInfo() *types.Info {
	return &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Uses:  map[*ast.Ident]types.Object{},
		Defs:  map[*ast.Ident]types.Object{},
	}
}

// AllASTs は、対象と対象外のファイルの構文木を元の順に返す。
func (p *Package) AllASTs() []*ast.File {
	files := make([]*ast.File, 0, len(p.Files)+len(p.Others))
	for _, f := range p.Files {
		files = append(files, f.AST)
	}
	for _, f := range p.Others {
		files = append(files, f.AST)
	}
	return files
}

// Check は、fset に属する files をこのパッケージとして型検査し、すべてのエラーを返す。
// info が nil なら型情報を記録しない。
func (p *Package) Check(fset *token.FileSet, files []*ast.File, info *types.Info) (*types.Package, []types.Error) {
	var errs []types.Error
	conf := types.Config{
		Importer: p.importer,
		Error: func(err error) {
			if terr, ok := errors.AsType[types.Error](err); ok {
				errs = append(errs, terr)
			}
		},
	}
	tpkg, _ := conf.Check(p.ImportPath, fset, files, info)
	return tpkg, errs
}
