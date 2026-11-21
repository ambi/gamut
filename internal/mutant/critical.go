package mutant

import (
	"fmt"
	"go/ast"
	"path"
	"strings"

	"github.com/ambi/gamut/internal/load"
)

// Critical は、ms のうち重要領域にある変異体の識別子を返す。重要領域の変異体は、すべて検出されるか受理されることを求める。
// 重要領域は、モジュール相対のパスが globs のどれかに当たるファイルと、宣言の Doc に //gamut:critical を持つ関数である。
// globs は path.Match の形で、要素 ** は 0 個以上のディレクトリに当たる。
// 関数の Doc の外の //gamut:critical は、警告として返して無視する。
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

// matchGlob は、/ 区切りのパス name が pattern に当たるかを返す。要素 ** は 0 個以上の要素に当たり、ほかの要素は path.Match で比べる。
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
