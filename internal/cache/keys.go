package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"hash"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
)

// Keys は、1 つのパッケージの変異体の判定を再利用してよいかを決める鍵を持つ。
// どの鍵も行と列に依存しない。コメントと空白だけの変更、宣言の並べ替え、別の宣言の追加では変わらない。
type Keys struct {
	pkg *load.Package
	// declIndex は、パッケージの最上位の宣言から、decls の番号を引く。
	declIndex map[ast.Decl]int
	decls     []string
	deps      []string
	imports   string
	tests     map[string]string
	testSeeds string
}

// NewKeys は、pkg の鍵を計算する。sources は load.SourceDeps が返した、パッケージの外の依存のファイルである。
func NewKeys(pkg *load.Package, sources []string) (*Keys, error) {
	k := &Keys{pkg: pkg, declIndex: map[ast.Decl]int{}}
	k.packageDecls()
	imports, err := hashFiles(pkg.ModuleDir, sources)
	if err != nil {
		return nil, err
	}
	k.imports = imports
	if err := k.testDecls(); err != nil {
		return nil, err
	}
	return k, nil
}

// Decl は、変異体を囲む最上位の宣言の、正規化した構文木のハッシュを返す。
func (k *Keys) Decl(m *mutant.Mutant) string {
	if i, ok := k.declOf(m); ok {
		return k.decls[i]
	}
	return ""
}

// Deps は、変異体を囲む宣言が go/types の参照で推移的に依存する、同じパッケージの宣言のハッシュを返す。
func (k *Keys) Deps(m *mutant.Mutant) string {
	if i, ok := k.declOf(m); ok {
		return k.deps[i]
	}
	return ""
}

// Imports は、パッケージの外の依存（手元のモジュールのパッケージ、埋め込みファイル、testdata）のハッシュを返す。
func (k *Keys) Imports() string { return k.imports }

// Test は、トップレベルのテスト name の関数と、それがテストファイルの中で参照する宣言のハッシュを返す。
// TestMain と init は、すべてのテストの鍵に含める。
func (k *Keys) Test(name string) string {
	if h, ok := k.tests[name]; ok {
		return h
	}
	return k.testSeeds
}

func (k *Keys) declOf(m *mutant.Mutant) (int, bool) {
	for _, d := range m.File.AST.Decls {
		if d.Pos() <= m.Site.Pos() && m.Site.End() <= d.End() {
			i, ok := k.declIndex[d]
			return i, ok
		}
	}
	return 0, false
}

// packageDecls は、パッケージの宣言のハッシュと、参照による依存のハッシュを計算する。
func (k *Keys) packageDecls() {
	info := k.pkg.Info
	var decls []ast.Decl
	for _, f := range k.pkg.AllASTs() {
		for _, d := range f.Decls {
			k.declIndex[d] = len(decls)
			decls = append(decls, d)
			k.decls = append(k.decls, hashNode(d, nil))
		}
	}
	// 宣言が定義するオブジェクトから宣言を引く。局所的な定義も含むが、自身を指すだけなので害はない。
	defined := map[types.Object]int{}
	methods := map[string][]int{}
	var inits []int
	for i, d := range decls {
		ast.Inspect(d, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if obj := info.Defs[id]; obj != nil {
					defined[obj] = i
				}
			}
			return true
		})
		if fd, ok := d.(*ast.FuncDecl); ok {
			switch {
			case fd.Recv != nil:
				methods[fd.Name.Name] = append(methods[fd.Name.Name], i)
			case fd.Name.Name == "init":
				inits = append(inits, i)
			}
		}
	}
	edges := make([][]int, len(decls))
	for i, d := range decls {
		// init はどの宣言からも参照されないが、パッケージの状態を変えうるので、すべての宣言の依存にする。
		edges[i] = slices.Clone(inits)
		ast.Inspect(d, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := origin(info.Uses[id])
			if obj == nil {
				return true
			}
			if j, ok := defined[obj]; ok {
				edges[i] = append(edges[i], j)
			}
			// インターフェースのメソッドの呼び出しは、パッケージの同名のメソッドのどれにも届きうる。
			if fn, ok := obj.(*types.Func); ok {
				if recv := fn.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
					edges[i] = append(edges[i], methods[fn.Name()]...)
				}
			}
			return true
		})
	}
	k.deps = make([]string, len(decls))
	for i := range decls {
		k.deps[i] = closureHash(i, edges, k.decls)
	}
}

// origin は、ジェネリックな型のインスタンスのメソッドやフィールドを、宣言されたオブジェクトに戻す。
func origin(obj types.Object) types.Object {
	switch o := obj.(type) {
	case *types.Func:
		return o.Origin()
	case *types.Var:
		return o.Origin()
	}
	return obj
}

// closureHash は、start から edges をたどって届く宣言（start 自身を除く）のハッシュをまとめる。
func closureHash(start int, edges [][]int, hashes []string) string {
	seen := map[int]bool{start: true}
	queue := []int{start}
	var members []string
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		for _, j := range edges[i] {
			if !seen[j] {
				seen[j] = true
				queue = append(queue, j)
				members = append(members, hashes[j])
			}
		}
	}
	slices.Sort(members)
	return hashStrings(members)
}

// testDecls は、テストファイルを読み、トップレベルのテストごとの鍵を計算する。
// テストファイルは型検査しない。テストだけが import するパッケージのエクスポートデータを持たないためである。
// 代わりに識別子の名前で参照を近似する。同名の局所変数も参照とみなすので、鍵は粗い側に倒れる。
func (k *Keys) testDecls() error {
	fset := token.NewFileSet()
	var hashes []string
	byName := map[string][]int{}
	var bodies []ast.Decl
	var seeds []int
	tops := map[string][]int{}
	for _, path := range k.pkg.TestFiles {
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, d := range f.Decls {
			i := len(bodies)
			bodies = append(bodies, d)
			var comments []*ast.CommentGroup
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && strings.HasPrefix(fd.Name.Name, "Example") {
				// Example の期待する出力はコメントに書かれる。
				comments = commentsWithin(f, d)
			}
			hashes = append(hashes, hashNode(d, comments))
			for _, name := range declNames(d) {
				byName[name] = append(byName[name], i)
			}
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				switch fd.Name.Name {
				case "TestMain", "init":
					seeds = append(seeds, i)
				default:
					tops[fd.Name.Name] = append(tops[fd.Name.Name], i)
				}
			}
		}
	}
	edges := make([][]int, len(bodies))
	for i, d := range bodies {
		ast.Inspect(d, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				edges[i] = append(edges[i], byName[id.Name]...)
			}
			return true
		})
	}
	// 起点を 1 つの仮の宣言にまとめ、その閉包のハッシュを鍵にする。
	root := len(bodies)
	edges = append(edges, nil)
	hashes = append(hashes, "")
	key := func(starts []int) string {
		edges[root] = slices.Concat(seeds, starts)
		return closureHash(root, edges, hashes)
	}
	k.testSeeds = key(nil)
	k.tests = map[string]string{}
	for name, starts := range tops {
		k.tests[name] = key(starts)
	}
	return nil
}

// declNames は、宣言が名前で参照されうる名前を返す。メソッドはメソッド名で引く。
func declNames(d ast.Decl) []string {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return []string{d.Name.Name}
	case *ast.GenDecl:
		var out []string
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.ValueSpec:
				for _, n := range s.Names {
					out = append(out, n.Name)
				}
			case *ast.TypeSpec:
				out = append(out, s.Name.Name)
			}
		}
		return out
	}
	return nil
}

func commentsWithin(f *ast.File, d ast.Decl) []*ast.CommentGroup {
	var out []*ast.CommentGroup
	for _, c := range f.Comments {
		if d.Pos() <= c.Pos() && c.End() <= d.End() {
			out = append(out, c)
		}
	}
	return out
}

// skippedTypes は、構文木のハッシュに含めないフィールドの型である。位置、コメント、構文だけによる名前の解決の結果を除く。
// 解決の結果は、構文木から宣言へ戻る参照を持つので、たどると循環する。
var skippedTypes = map[string]bool{"token.Pos": true, "*ast.CommentGroup": true, "*ast.Object": true, "*ast.Scope": true}

// hashNode は、位置とコメントを除いた構文木のハッシュを返す。comments を渡すと、その本文も含める。
func hashNode(n ast.Node, comments []*ast.CommentGroup) string {
	h := sha256.New()
	writeValue(h, reflect.ValueOf(n))
	for _, c := range comments {
		writeString(h, c.Text())
	}
	return digest(h)
}

// writeValue は、構文木の値を、ノードの型、字句、名前、子の並びとして書く。
// 位置、コメント、識別子の解決結果は書かない。
func writeValue(h hash.Hash, v reflect.Value) {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			writeString(h, "nil")
			return
		}
		writeValue(h, v.Elem())
	case reflect.Struct:
		writeString(h, v.Type().Name())
		for sf, f := range v.Fields() {
			if !skippedTypes[sf.Type.String()] {
				writeValue(h, f)
			}
		}
	case reflect.Slice:
		writeInt(h, int64(v.Len()))
		for i := range v.Len() {
			writeValue(h, v.Index(i))
		}
	case reflect.String:
		writeString(h, v.String())
	case reflect.Bool:
		if v.Bool() {
			writeInt(h, 1)
		} else {
			writeInt(h, 0)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		writeInt(h, v.Int())
	}
}

func writeString(h hash.Hash, s string) {
	writeInt(h, int64(len(s)))
	h.Write([]byte(s))
}

func writeInt(h hash.Hash, n int64) {
	h.Write(binary.AppendVarint(nil, n))
}

func hashStrings(parts []string) string {
	h := sha256.New()
	for _, p := range parts {
		writeString(h, p)
	}
	return digest(h)
}

// hashFiles は、ファイルのモジュール相対のパスと中身のハッシュを返す。
func hashFiles(moduleDir string, paths []string) (string, error) {
	h := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(moduleDir, path)
		if err != nil {
			rel = path
		}
		writeString(h, filepath.ToSlash(rel))
		writeString(h, string(data))
	}
	return digest(h), nil
}

// digest は、キャッシュの大きさを抑えるため、ハッシュを 64 ビットに切り詰める。変更の検出には足りる。
func digest(h hash.Hash) string {
	return hex.EncodeToString(h.Sum(nil)[:8])
}
