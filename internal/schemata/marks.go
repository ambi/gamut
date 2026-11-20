package schemata

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ambi/gamut/internal/load"
)

// TestMarks は、テストファイルの書き換えである。トップレベルのテスト関数の先頭に、記録の境目を書く呼び出しを差し込む。
// 1 つのプロセスで全テストを実行しても、到達と感染をテストごとに振り分けられるようにする。
type TestMarks struct {
	// Files は、テストファイルの絶対パスから書き換え後の内容への対応である。
	Files map[string][]byte
	// Batchable は、1 つのプロセスでまとめて被覆を記録できるテストの名前である。-test.parallel=1 で実行することを前提にする。
	// 本文の文として Parallel を呼ぶテストは、再開の直後にも境目を書くので含める。
	// そのうえで本文の中（サブテスト）でも Parallel を呼ぶテストは、サブテストの間に別の並列のテストが再開しうるので含めない。
	Batchable map[string]bool
}

// MarkTests は、pkg のテストファイルを書き換える。行は変えない。
// 外部テスト（package <名前>_test）は、対象パッケージを import しているファイルだけ書き換え、公開した補助関数を呼ぶ。
func MarkTests(pkg *load.Package) (*TestMarks, error) {
	marks := &TestMarks{Files: map[string][]byte{}, Batchable: map[string]bool{}}
	fset := token.NewFileSet()
	for _, path := range pkg.TestFiles {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		call := TestStartName
		if f.Name.Name != pkg.Name {
			name, ok := importName(f, pkg)
			if !ok {
				continue
			}
			call = name + "." + TestStartName
		}
		type insert struct {
			offset int
			text   string
		}
		var inserts []insert
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil || !isTestName(fd.Name.Name) {
				continue
			}
			text := fmt.Sprintf(" %s(%s);", call, strconv.Quote(fd.Name.Name))
			inserts = append(inserts, insert{offset: fset.Position(fd.Body.Lbrace).Offset + 1, text: text})
			top := 0
			for _, st := range fd.Body.List {
				if isParallelCall(st) {
					// 並列のテストは Parallel で止まり、ほかのテストの後に再開する。再開した所を境目にする。
					top++
					inserts = append(inserts, insert{offset: fset.Position(st.End()).Offset, text: ";" + text})
				}
			}
			if top == 0 || countParallel(fd.Body) == top {
				marks.Batchable[fd.Name.Name] = true
			}
		}
		if len(inserts) == 0 {
			continue
		}
		var buf bytes.Buffer
		pos := 0
		for _, in := range inserts {
			buf.Write(src[pos:in.offset])
			buf.WriteString(in.text)
			pos = in.offset
		}
		buf.Write(src[pos:])
		marks.Files[path] = buf.Bytes()
	}
	return marks, nil
}

// importName は、外部テストのファイル f が対象パッケージを import する名前を返す。
func importName(f *ast.File, pkg *load.Package) (string, bool) {
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != pkg.ImportPath {
			continue
		}
		if spec.Name == nil {
			return pkg.Name, true
		}
		if spec.Name.Name == "_" || spec.Name.Name == "." {
			return "", false
		}
		return spec.Name.Name, true
	}
	return "", false
}

// isTestName は、name が go test の実行するテスト関数の名前の形かを返す。
func isTestName(name string) bool {
	for _, prefix := range []string{"Test", "Fuzz", "Example"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			return name != "TestMain" && (rest == "" || !startsLower(rest))
		}
	}
	return false
}

// isParallelCall は、文が X.Parallel() の呼び出しだけからなるかを返す。
func isParallelCall(st ast.Stmt) bool {
	es, ok := st.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Parallel"
}

// countParallel は、本文の中の Parallel というメソッドの呼び出しの数を返す。
func countParallel(body *ast.BlockStmt) int {
	n := 0
	ast.Inspect(body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Parallel" {
				n++
			}
		}
		return true
	})
	return n
}

// startsLower は、s が小文字で始まるかを返す。
func startsLower(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLower(r)
}
