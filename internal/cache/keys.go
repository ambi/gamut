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
	"unicode"
	"unicode/utf8"

	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
)

// Keys holds the keys deciding whether the verdicts of one package's mutants may be reused.
// No key depends on lines and columns. Changes to comments or whitespace alone, reordered declarations, and added unrelated declarations leave them unchanged.
type Keys struct {
	pkg *load.Package
	// declIndex maps the package's top-level declarations to their numbers in decls; declAt is the reverse.
	declIndex map[ast.Decl]int
	declAt    []ast.Decl
	decls     []string
	deps      []string
	// local are the hashes of the declarations (constants, types, variables) a declaration depends on without going through function declarations.
	local     []string
	imports   string
	tests     map[string]string
	testSeeds string
	// inits are the declarations run by package initialization (package variables and init) and the declarations of generated files.
	inits []int
	// testSpans are the ranges of the test files' declarations, testClosures the test file declarations each top-level test refers to transitively,
	// and testSeedDecls the declarations included for every test (TestMain and init).
	testSpans     []fileSpan
	testClosures  map[string][]int
	testSeedDecls []int
	// testFuncs are the names of the top-level test functions (Test, Fuzz, Example) in the test files.
	testFuncs []string
}

type fileSpan struct {
	path       string
	start, end int
}

// NewKeys computes pkg's keys. sources are the files of the dependencies outside the package, as load.SourceDeps returns them.
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

// Decl returns the hash of the normalized syntax tree of the top-level declaration enclosing the mutant.
func (k *Keys) Decl(m *mutant.Mutant) string {
	if i, ok := k.declOf(m); ok {
		return k.decls[i]
	}
	return ""
}

// Deps returns the hash of the declarations of the same package that the enclosing declaration depends on transitively through go/types references.
func (k *Keys) Deps(m *mutant.Mutant) string {
	if i, ok := k.declOf(m); ok {
		return k.deps[i]
	}
	return ""
}

// Viability is the key deciding whether a mutant's viability (whether it type-checks) may be reused.
// Replacing an operator can cause type errors only inside the enclosing declaration or in declarations using its constant value (such as array lengths).
// The former is caught by the keys of the enclosing declaration, its dependencies, and external dependencies. The latter only happens to mutants of constant declarations, which use the key of every declaration of the package.
func (k *Keys) Viability(m *mutant.Mutant) string {
	i, ok := k.declOf(m)
	if !ok {
		return ""
	}
	if gd, isGen := k.declAt[i].(*ast.GenDecl); isGen && gd.Tok == token.CONST {
		return hashStrings(append(slices.Clone(k.decls), k.imports))
	}
	return hashStrings([]string{k.decls[i], k.deps[i], k.imports})
}

// Imports returns the hash of the dependencies outside the package (local module packages, embedded files, testdata).
func (k *Keys) Imports() string { return k.imports }

// Reach is the ReachOf value of the declaration enclosing the mutant. Coverage recording uses it to check whether a declaration a test reached changed.
func (k *Keys) Reach(m *mutant.Mutant) string {
	if i, ok := k.declOf(m); ok {
		return hashStrings([]string{k.decls[i], k.local[i]})
	}
	return ""
}

// ReachOf returns the key combining the package declaration d and the declarations (constants, types, variables) d depends on without going through function declarations.
// It returns empty when d is not a declaration of the package.
// Coverage recording records every function a test entered, so a change to a called function is caught by that function's key.
// Including called functions as dependencies would remeasure tests that merely entered the caller without entering the changed function.
func (k *Keys) ReachOf(d ast.Decl) string {
	i, ok := k.declIndex[d]
	if !ok {
		return ""
	}
	return hashStrings([]string{k.decls[i], k.local[i]})
}

// Reaches returns the set of ReachOf values of every declaration of the package.
func (k *Keys) Reaches() map[string]bool {
	out := map[string]bool{}
	for d := range k.declIndex {
		out[k.ReachOf(d)] = true
	}
	return out
}

// Init is the key of the declarations run by package initialization (package variables and init), the declarations of generated files, and their dependencies.
// Mutation sites reached during initialization appear in every test's record, and entering functions of generated files is not recorded,
// so a change to it remeasures the coverage of every test.
func (k *Keys) Init() string {
	var parts []string
	for _, i := range k.inits {
		parts = append(parts, k.decls[i], k.deps[i])
	}
	slices.Sort(parts)
	return hashStrings(parts)
}

// ChangedTests returns the names of the top-level tests that refer transitively to a test file declaration with a range for which intersects returns true.
// When TestMain or init changed, it returns every test.
func (k *Keys) ChangedTests(intersects func(path string, start, end int) bool) map[string]bool {
	changed := make([]bool, len(k.testSpans))
	for i, sp := range k.testSpans {
		changed[i] = intersects(sp.path, sp.start, sp.end)
	}
	all := slices.ContainsFunc(k.testSeedDecls, func(i int) bool { return changed[i] })
	out := map[string]bool{}
	for name, closure := range k.testClosures {
		if all || slices.ContainsFunc(closure, func(i int) bool { return changed[i] }) {
			out[name] = true
		}
	}
	return out
}

// reachable returns starts and the vertices reachable from them along edges.
func reachable(starts []int, edges [][]int) []int {
	seen := map[int]bool{}
	var out []int
	queue := slices.Clone(starts)
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		if seen[i] {
			continue
		}
		seen[i] = true
		out = append(out, i)
		queue = append(queue, edges[i]...)
	}
	return out
}

// Test returns the hash of the function of the top-level test name and the declarations it refers to in the test files.
// TestMain and init are included in every test's key.
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

// packageDecls computes the hashes of the package's declarations and of their dependencies by reference.
func (k *Keys) packageDecls() {
	info := k.pkg.Info
	generated := map[ast.Decl]bool{}
	for _, f := range k.pkg.Others {
		for _, d := range f.AST.Decls {
			generated[d] = true
		}
	}
	var decls []ast.Decl
	for _, f := range k.pkg.AllASTs() {
		for _, d := range f.Decls {
			k.declIndex[d] = len(decls)
			k.declAt = append(k.declAt, d)
			decls = append(decls, d)
			k.decls = append(k.decls, hashNode(d, nil))
		}
	}
	// Map the objects a declaration defines back to it. Local definitions are included too, harmlessly, as they point only at their own declaration.
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
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.VAR || generated[d] {
			k.inits = append(k.inits, i)
		}
	}
	k.inits = append(k.inits, inits...)
	edges := make([][]int, len(decls))
	for i, d := range decls {
		// No declaration refers to init, but it may change the package state, so it is a dependency of every declaration.
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
			// A call of an interface method may reach any method of the same name in the package.
			if fn, ok := obj.(*types.Func); ok {
				if recv := fn.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
					edges[i] = append(edges[i], methods[fn.Name()]...)
				}
			}
			return true
		})
	}
	k.deps = make([]string, len(decls))
	k.local = make([]string, len(decls))
	nonFunc := make([][]int, len(decls))
	for i, es := range edges {
		for _, j := range es {
			if _, ok := decls[j].(*ast.FuncDecl); !ok {
				nonFunc[i] = append(nonFunc[i], j)
			}
		}
	}
	for i := range decls {
		k.deps[i] = closureHash(i, edges, k.decls)
		k.local[i] = closureHash(i, nonFunc, k.decls)
	}
}

// origin maps methods and fields of instances of generic types back to the declared objects.
func origin(obj types.Object) types.Object {
	switch o := obj.(type) {
	case *types.Func:
		return o.Origin()
	case *types.Var:
		return o.Origin()
	}
	return obj
}

// closureHash combines the hashes of the declarations reachable from start along edges (start itself excluded).
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

// testDecls reads the test files and computes the key of each top-level test.
// Test files are not type-checked, because export data is missing for packages only tests import.
// References are approximated by identifier names instead. Local variables of the same name count as references, so the keys err on the coarse side.
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
			k.testSpans = append(k.testSpans, fileSpan{path: path, start: fset.Position(d.Pos()).Offset, end: fset.Position(d.End()).Offset})
			var comments []*ast.CommentGroup
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && strings.HasPrefix(fd.Name.Name, "Example") {
				// An Example's expected output is written in comments.
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
					if _, seen := tops[fd.Name.Name]; !seen && isTestFunc(fd.Name.Name) {
						k.testFuncs = append(k.testFuncs, fd.Name.Name)
					}
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
	// Gather the starting points into one pseudo declaration and use the hash of its closure as the key.
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

	k.testSeedDecls = seeds
	k.testClosures = map[string][]int{}
	for name, starts := range tops {
		k.testClosures[name] = reachable(slices.Concat(seeds, starts), edges[:root])
	}
	return nil
}

// RevDepKey returns the test key of a reverse dependency: one per package rather than per test, erring on the coarse side.
// files are the reverse dependency's Go and test files and the files of the local packages it depends on (except the target).
func RevDepKey(moduleDir string, files []string) (string, error) {
	return hashFiles(moduleDir, files)
}

// TestFuncs returns the names of the top-level test functions (Test, Fuzz, Example) in the test files, in the file and declaration order of go test -list.
func (k *Keys) TestFuncs() []string { return k.testFuncs }

// isTestFunc reports whether name has the form of a test function go test runs. A lowercase letter right after the prefix rules it out (as in Testify).
func isTestFunc(name string) bool {
	for _, prefix := range []string{"Test", "Fuzz", "Example"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			return rest == "" || !startsLower(rest)
		}
	}
	return false
}

// declNames returns the names by which a declaration may be referred to. Methods are looked up by method name.
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

// skippedTypes are the field types left out of syntax tree hashes: positions, comments, and the results of syntax-only name resolution.
// Resolution results point from the syntax tree back to declarations, so following them would loop.
var skippedTypes = map[string]bool{"token.Pos": true, "*ast.CommentGroup": true, "*ast.Object": true, "*ast.Scope": true}

// hashNode returns the hash of the syntax tree without positions and comments. With comments, their text is included too.
func hashNode(n ast.Node, comments []*ast.CommentGroup) string {
	h := sha256.New()
	writeValue(h, reflect.ValueOf(n))
	for _, c := range comments {
		writeString(h, c.Text())
	}
	return digest(h)
}

// writeValue writes a syntax tree value as the node type, tokens, names, and children.
// Positions, comments, and identifier resolution results are not written.
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

// hashFiles returns the hash of the files' module-relative paths and contents.
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

// digest truncates a hash to 64 bits to keep the cache small, which is enough to detect changes.
func digest(h hash.Hash) string {
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// startsLower reports whether s starts with a lowercase letter.
func startsLower(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLower(r)
}
