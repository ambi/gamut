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
	"maps"
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
	DepOnly      bool
	GoFiles      []string
	CgoFiles     []string
	TestGoFiles  []string
	XTestGoFiles []string
	ImportMap    map[string]string
	Imports      []string
	Module       *struct {
		Path string
		Dir  string
	}
	Error *struct{ Err string }
}

// Load は、ビルドタグ tags の下で patterns に当たるパッケージを読み、型を検査する。
func Load(patterns []string, tags []string) ([]*Package, error) {
	pkgs, _, err := WithHint(patterns, tags, nil)
	return pkgs, err
}

// listFields は、読み込みが go list から受け取るフィールドである。
const listFields = "Dir,ImportPath,Name,DepOnly,GoFiles,CgoFiles,TestGoFiles,XTestGoFiles,ImportMap,Imports,Module,Error"

// WithHint は、Load と同じく読み込む。型検査が要るエクスポートデータを持つパッケージの import パスを、次の読み込みの hint として返す。
//
// エクスポートデータは、対象パッケージの依存の分だけ求める。go list -export -deps を対象パッケージごと実行すると、
// 編集したばかりの対象パッケージまでコンパイルしてビルドキャッシュに書くが、その結果はどこにも使わない
// （schemata のバイナリは書き換えたソースからコンパイルし直す）。そこで、依存の一覧（-export なし）と、
// hint のパッケージのエクスポートデータとを並行して求め、hint に足りないものだけを後から求める。
// hint が前回と同じなら、go list の 2 回の実行は並行するので、待つのは 1 回分である。
func WithHint(patterns, tags, hint []string) ([]*Package, []string, error) {
	if len(patterns) == 0 {
		return nil, nil, errors.New("no package patterns")
	}
	abs, err := AbsPatterns(patterns)
	if err != nil {
		return nil, nil, err
	}
	runDir := strings.TrimSuffix(abs[0], filepath.FromSlash("/..."))
	exports := map[string]string{}
	var hinted sync.WaitGroup
	var hintErr error
	if len(hint) > 0 {
		hinted.Go(func() { hintErr = listExports(runDir, tags, hint, exports) })
	}
	// schemata の補助ファイルが import するパッケージも、型検査のために一緒に列挙する。
	args := []string{"list", "-e", "-json=" + listFields, "-deps"}
	if len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, ","))
	}
	args = append(append(args, abs...), HelperImports...)
	out, err := goList(runDir, args)
	hinted.Wait()
	if err != nil {
		return nil, nil, err
	}
	var targets []*listedPackage
	var all []*listedPackage
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listedPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, nil, fmt.Errorf("decode go list output: %w", err)
		}
		if p.Error != nil {
			return nil, nil, fmt.Errorf("go list %s: %s", p.ImportPath, p.Error.Err)
		}
		all = append(all, &p)
		if !p.DepOnly && !slices.Contains(HelperImports, p.ImportPath) && len(p.GoFiles)+len(p.CgoFiles) > 0 {
			targets = append(targets, &p)
		}
	}
	if len(targets) == 0 {
		return nil, nil, fmt.Errorf("no packages with non-test Go files match %s", strings.Join(patterns, " "))
	}
	slices.SortFunc(targets, func(a, b *listedPackage) int { return strings.Compare(a.ImportPath, b.ImportPath) })

	// エクスポートデータが要るのは、対象パッケージ以外のすべてと、別の対象パッケージが import する対象パッケージである。
	isTarget := map[string]bool{}
	for _, t := range targets {
		isTarget[t.ImportPath] = true
	}
	imported := map[string]bool{}
	for _, t := range targets {
		for _, path := range t.Imports {
			if mapped, ok := t.ImportMap[path]; ok {
				path = mapped
			}
			imported[path] = true
		}
	}
	var needed, missing []string
	for _, p := range all {
		if isTarget[p.ImportPath] && !imported[p.ImportPath] || p.ImportPath == "unsafe" {
			continue
		}
		needed = append(needed, p.ImportPath)
		if hintErr != nil || exports[p.ImportPath] == "" {
			missing = append(missing, p.ImportPath)
		}
	}
	if len(missing) > 0 {
		if err := listExports(runDir, tags, missing, exports); err != nil {
			return nil, nil, err
		}
	}
	fset := token.NewFileSet()
	shared := &sharedImporter{exports: exports}
	shared.imp = importer.ForCompiler(fset, "gc", shared.lookup)
	var pkgs []*Package
	for _, target := range targets {
		if len(target.CgoFiles) > 0 {
			return nil, nil, fmt.Errorf("package %s uses cgo, which gamut does not support", target.ImportPath)
		}
		if target.Module == nil {
			return nil, nil, fmt.Errorf("package %s is not in a module", target.ImportPath)
		}
		if len(pkgs) > 0 && pkgs[0].ModuleDir != target.Module.Dir {
			// 報告のファイルのパスはモジュール相対なので、モジュールをまたぐと区別できない。
			return nil, nil, fmt.Errorf("packages %s and %s are in different modules; run gamut once per module",
				pkgs[0].ImportPath, target.ImportPath)
		}
		pkg, err := newPackage(target, tags, fset, &packageImporter{shared: shared, importMap: target.ImportMap})
		if err != nil {
			return nil, nil, err
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, needed, nil
}

// goList は、dir で go に args を渡して実行し、標準出力を返す。
func goList(dir string, args []string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// listExports は、paths のパッケージのエクスポートデータのファイルを求め、exports に足す。
// 求められなかったパッケージ（消えたパッケージなど）は足さない。
func listExports(dir string, tags, paths []string, exports map[string]string) error {
	args := []string{"list", "-e", "-export", "-json=ImportPath,Export"}
	if len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, ","))
	}
	out, err := goList(dir, append(args, paths...))
	if err != nil {
		return err
	}
	found := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p struct{ ImportPath, Export string }
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return fmt.Errorf("decode go list output: %w", err)
		}
		if p.Export != "" {
			found[p.ImportPath] = p.Export
		}
	}
	maps.Copy(exports, found)
	return nil
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

// AbsPatterns は、パターンのディレクトリを絶対パスにする。<ディレクトリ>/... の形は保つ。
func AbsPatterns(patterns []string) ([]string, error) {
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
	return abs, nil
}

// ModuleRoot は、dir から親へたどって最初に go.mod を持つディレクトリを返す。見つからなければ空を返す。
func ModuleRoot(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
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
