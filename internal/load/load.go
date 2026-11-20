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
	"slices"
	"strings"
	"sync"
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
	// Tags は、読み込みとビルドに渡すビルドタグである。
	Tags []string
	// HasTests は、パッケージがテストファイル（外部テストを含む）を持つかである。
	HasTests bool
	// TestFiles は、テストファイル（外部テストを含む）の絶対パスである。
	TestFiles []string
	// Fset はすべての構文木の位置を持つ。1 回の Load で読んだパッケージはこれを共有する。
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
	Dir          string
	ImportPath   string
	Name         string
	Export       string
	DepOnly      bool
	GoFiles      []string
	CgoFiles     []string
	TestGoFiles  []string
	XTestGoFiles []string
	ImportMap    map[string]string
	Module       *struct {
		Path string
		Dir  string
	}
	Error *struct{ Err string }
}

// Load は、ビルドタグ tags の下で patterns に当たるパッケージを読み、型を検査する。
// パターンはパッケージディレクトリか、その下のすべてのパッケージを表す <ディレクトリ>/... である。
// go list は最初のパターンのディレクトリで 1 回だけ実行し、依存のエクスポートデータと型の読み込みは全パッケージで共有する。
// 対象のファイルは go list の GoFiles に従うので、ビルドの条件を自前で評価しない。
// テスト以外の Go ファイルを持たないパッケージは返さない。返すパッケージは import パスの順に並ぶ。
func Load(patterns []string, tags []string) ([]*Package, error) {
	if len(patterns) == 0 {
		return nil, errors.New("no package patterns")
	}
	var abs []string
	for _, p := range patterns {
		dir, rest := p, ""
		if d, ok := strings.CutSuffix(filepath.ToSlash(p), "/..."); ok {
			dir, rest = filepath.FromSlash(d), "/..."
		} else if p == "..." {
			dir, rest = ".", "/..."
		}
		a, err := filepath.Abs(dir)
		if err != nil {
			return nil, err
		}
		abs = append(abs, a+filepath.FromSlash(rest))
	}
	runDir := strings.TrimSuffix(abs[0], filepath.FromSlash("/..."))
	// schemata の補助ファイルが import するパッケージも、型検査のために一緒に列挙する。
	args := []string{"list", "-e", "-json", "-export", "-deps"}
	if len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, ","))
	}
	args = append(append(args, abs...), HelperImports...)
	cmd := exec.Command("go", args...)
	cmd.Dir = runDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list in %s: %w: %s", runDir, err, strings.TrimSpace(stderr.String()))
	}
	exports := map[string]string{}
	var targets []*listedPackage
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
		if !p.DepOnly && !slices.Contains(HelperImports, p.ImportPath) && len(p.GoFiles)+len(p.CgoFiles) > 0 {
			targets = append(targets, &p)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no packages with non-test Go files match %s", strings.Join(patterns, " "))
	}
	slices.SortFunc(targets, func(a, b *listedPackage) int { return strings.Compare(a.ImportPath, b.ImportPath) })

	fset := token.NewFileSet()
	shared := &sharedImporter{exports: exports}
	shared.imp = importer.ForCompiler(fset, "gc", shared.lookup)
	var pkgs []*Package
	for _, target := range targets {
		if len(target.CgoFiles) > 0 {
			return nil, fmt.Errorf("package %s uses cgo, which gamut does not support", target.ImportPath)
		}
		if target.Module == nil {
			return nil, fmt.Errorf("package %s is not in a module", target.ImportPath)
		}
		if len(pkgs) > 0 && pkgs[0].ModuleDir != target.Module.Dir {
			// 報告のファイルのパスはモジュール相対なので、モジュールをまたぐと区別できない。
			return nil, fmt.Errorf("packages %s and %s are in different modules; run gamut once per module",
				pkgs[0].ImportPath, target.ImportPath)
		}
		pkg, err := newPackage(target, tags, fset, &packageImporter{shared: shared, importMap: target.ImportMap})
		if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

func newPackage(target *listedPackage, tags []string, fset *token.FileSet, imp types.Importer) (*Package, error) {
	pkg := &Package{
		Dir:        target.Dir,
		ImportPath: target.ImportPath,
		Name:       target.Name,
		ModuleDir:  target.Module.Dir,
		ModulePath: target.Module.Path,
		Tags:       tags,
		HasTests:   len(target.TestGoFiles)+len(target.XTestGoFiles) > 0,
		Fset:       fset,
		importer:   imp,
	}
	for _, name := range slices.Concat(target.TestGoFiles, target.XTestGoFiles) {
		pkg.TestFiles = append(pkg.TestFiles, filepath.Join(target.Dir, name))
	}
	for _, name := range target.GoFiles {
		path := filepath.Join(target.Dir, name)
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
	info := NewInfo()
	tpkg, errs := pkg.Check(pkg.Fset, pkg.AllASTs(), info)
	if len(errs) > 0 {
		return nil, fmt.Errorf("type-check %s: %w", pkg.ImportPath, errs[0])
	}
	pkg.Types = tpkg
	pkg.Info = info
	return pkg, nil
}

// sharedImporter は、エクスポートデータから読んだ types.Package を全対象パッケージで共有する。
// gc の importer は並行に使えないので、mutex で守る。
// 対象パッケージが別の対象パッケージを import するときも、書き換え前のエクスポートデータを読む。
type sharedImporter struct {
	mu      sync.Mutex
	imp     types.Importer
	exports map[string]string
	// importMap は、mu を持つ呼び出しの間だけ、import するパッケージの ImportMap を指す。
	importMap map[string]string
}

func (s *sharedImporter) lookup(path string) (io.ReadCloser, error) {
	if mapped, ok := s.importMap[path]; ok {
		path = mapped
	}
	export, ok := s.exports[path]
	if !ok || export == "" {
		return nil, fmt.Errorf("no export data for %s", path)
	}
	return os.Open(export)
}

// packageImporter は、1 つの対象パッケージの ImportMap で共有の importer を引く。
type packageImporter struct {
	shared    *sharedImporter
	importMap map[string]string
}

func (p *packageImporter) Import(path string) (*types.Package, error) {
	p.shared.mu.Lock()
	defer p.shared.mu.Unlock()
	p.shared.importMap = p.importMap
	return p.shared.imp.Import(path)
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
