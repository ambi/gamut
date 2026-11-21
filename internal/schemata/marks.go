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

// TestMarks is the rewrite of the test files. It inserts, at the start of each top-level test function, a call that writes a boundary in the record.
// Reach and infection can then be attributed to each test even when one process runs every test.
type TestMarks struct {
	// Files maps the absolute paths of the test files to their rewritten contents.
	Files map[string][]byte
	// Batchable are the names of the tests whose coverage can be recorded together in one process. It assumes running with -test.parallel=1.
	// Tests that call Parallel as a statement of their body are included, because a boundary is also written right after they resume.
	// Tests that also call Parallel deeper in their body (in subtests) are not included, because another parallel test may resume between the subtests.
	Batchable map[string]bool
}

// MarkTests rewrites pkg's test files without changing lines.
// For external tests (package <name>_test), only files that import the target package are rewritten, and they call the exported helper function.
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
					// A parallel test pauses at Parallel and resumes after the other tests. Where it resumes becomes a boundary.
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

// importName returns the name under which the external test file f imports the target package.
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

// isTestName reports whether name has the form of a test function name go test runs.
func isTestName(name string) bool {
	for _, prefix := range []string{"Test", "Fuzz", "Example"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			return name != "TestMain" && (rest == "" || !startsLower(rest))
		}
	}
	return false
}

// isParallelCall reports whether the statement is just a call to X.Parallel().
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

// countParallel returns the number of calls to a method named Parallel in the body.
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

// startsLower reports whether s starts with a lowercase letter.
func startsLower(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLower(r)
}
