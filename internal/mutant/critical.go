package mutant

import (
	"fmt"
	"go/ast"
	"path"
	"strings"

	"github.com/ambi/gamut/internal/load"
)

// Critical returns the identifiers of the mutants in ms that are in a critical region. Every mutant in a critical region must be detected or accepted.
// Critical regions are files whose module-relative path matches one of globs, and functions with //gamut:critical in the Doc of their declaration.
// globs are in path.Match form, and the element ** matches zero or more directories.
// //gamut:critical outside a function's Doc is returned as a warning and ignored.
func Critical(pkg *load.Package, ms []*Mutant, globs []string) (map[string]bool, []string, error) {
	for _, g := range globs {
		for elem := range strings.SplitSeq(g, "/") {
			if _, err := path.Match(elem, ""); err != nil {
				return nil, nil, fmt.Errorf("critical: bad pattern %q: %w", g, err)
			}
		}
	}
	type span struct {
		file       *load.File
		first, end int
	}
	var spans []span
	var warnings []string
	for _, f := range pkg.Files {
		inDoc := map[*ast.Comment]*ast.FuncDecl{}
		for _, d := range f.AST.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Doc != nil {
				for _, c := range fd.Doc.List {
					inDoc[c] = fd
				}
			}
		}
		for _, group := range f.AST.Comments {
			for _, c := range group.List {
				rest, ok := strings.CutPrefix(c.Text, directivePrefix)
				if !ok {
					continue
				}
				if name, _, _ := strings.Cut(rest, " "); name != criticalDirective {
					continue
				}
				fd := inDoc[c]
				if fd == nil {
					warnings = append(warnings, fmt.Sprintf("%s:%d: %s%s must be in a function's doc comment; ignored",
						f.Rel, pkg.Fset.Position(c.Slash).Line, directivePrefix, criticalDirective))
					continue
				}
				spans = append(spans, span{f, pkg.Fset.Position(fd.Pos()).Line, pkg.Fset.Position(fd.End()).Line})
			}
		}
	}
	out := map[string]bool{}
	for _, m := range ms {
		for _, g := range globs {
			if ok, _ := matchGlob(g, m.File.Rel); ok {
				out[m.ID] = true
			}
		}
		for _, s := range spans {
			if m.File == s.file && s.first <= m.Line && m.Line <= s.end {
				out[m.ID] = true
			}
		}
	}
	return out, warnings, nil
}

// matchGlob reports whether the /-separated path name matches pattern. The element ** matches zero or more elements, and other elements are compared with path.Match.
func matchGlob(pattern, name string) (bool, error) {
	pp := strings.Split(pattern, "/")
	var np []string
	if name != "" {
		np = strings.Split(name, "/")
	}
	return matchParts(pp, np)
}

func matchParts(pp, np []string) (bool, error) {
	if len(pp) == 0 {
		return len(np) == 0, nil
	}
	if pp[0] == "**" {
		for i := 0; i <= len(np); i++ {
			if ok, err := matchParts(pp[1:], np[i:]); ok || err != nil {
				return ok, err
			}
		}
		return false, nil
	}
	if len(np) == 0 {
		return false, nil
	}
	ok, err := path.Match(pp[0], np[0])
	if err != nil || !ok {
		return false, err
	}
	return matchParts(pp[1:], np[1:])
}
