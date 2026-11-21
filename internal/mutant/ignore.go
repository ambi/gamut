package mutant

import (
	"bytes"
	"fmt"
	"go/ast"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/load"
)

// The directives of disable comments. Following the convention of Go directive comments (//go:, //nolint:), there is no space right after //.
const (
	directivePrefix = "//gamut:"
	ignoreDirective = "ignore"
	ignoreStart     = "ignore-start"
	ignoreEnd       = "ignore-end"
	// criticalDirective is the directive that makes a function a critical region. Critical reads it.
	criticalDirective = "critical"
)

// Ignored is a mutant stopped by a disable comment, with the reason written in the comment.
type Ignored struct {
	Mutant *Mutant
	Reason string
}

// ignoreRule is the range of lines and the mutators one disable comment stops.
type ignoreRule struct {
	file       *load.File
	first, end int // the lines stopped (both ends included)
	types      []Type
	reason     string
}

// Ignores reads the disable comments of pkg's target files and splits ms into those to judge, kept, and those stopped, ignored.
// kept is renumbered from 1. Comments without a reason and unmatched ranges are returned as warnings, and reading continues.
// An unknown mutator name is an error, because a misspelling would otherwise silently stop every mutator.
//
// The ranges a comment stops are:
//   - //gamut:ignore [TYPE,...] [-- reason]: the same line if it has code, otherwise the next line. As the comment right before a function declaration (Doc), the whole function.
//   - From //gamut:ignore-start [TYPE,...] [-- reason] to //gamut:ignore-end: the lines between them. Without an end, up to the end of the file.
func Ignores(pkg *load.Package, ms []*Mutant) (kept []*Mutant, ignored []Ignored, warnings []string, err error) {
	var rules []ignoreRule
	for _, f := range pkg.Files {
		rs, ws, err := fileIgnores(pkg, f)
		if err != nil {
			return nil, nil, nil, err
		}
		rules = append(rules, rs...)
		warnings = append(warnings, ws...)
	}
	if len(rules) == 0 {
		return ms, nil, warnings, nil
	}
	for _, m := range ms {
		i := slices.IndexFunc(rules, func(r ignoreRule) bool { return r.matches(m) })
		if i < 0 {
			kept = append(kept, m)
			continue
		}
		ignored = append(ignored, Ignored{Mutant: m, Reason: rules[i].reason})
	}
	number(kept)
	return kept, ignored, warnings, nil
}

func (r ignoreRule) matches(m *Mutant) bool {
	return m.File == r.file && r.first <= m.Line && m.Line <= r.end && (len(r.types) == 0 || slices.Contains(r.types, m.Type))
}

// fileIgnores reads the disable comments of the file f.
func fileIgnores(pkg *load.Package, f *load.File) ([]ignoreRule, []string, error) {
	var rules []ignoreRule
	var warnings []string
	lastLine := pkg.Fset.Position(f.AST.FileEnd).Line
	// docOf maps a comment in the Doc of a function declaration to that function.
	docOf := map[*ast.Comment]*ast.FuncDecl{}
	for _, d := range f.AST.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Doc != nil {
			for _, c := range fd.Doc.List {
				docOf[c] = fd
			}
		}
	}
	var open *ignoreRule
	for _, group := range f.AST.Comments {
		for _, c := range group.List {
			rest, ok := strings.CutPrefix(c.Text, directivePrefix)
			if !ok {
				continue
			}
			pos := pkg.Fset.Position(c.Slash)
			where := fmt.Sprintf("%s:%d", f.Rel, pos.Line)
			name, args, _ := strings.Cut(rest, " ")
			switch name {
			case ignoreDirective, ignoreStart:
				types, reason, err := parseIgnoreArgs(args)
				if err != nil {
					return nil, nil, fmt.Errorf("%s: %w", where, err)
				}
				if reason == "" {
					warnings = append(warnings, where+": "+directivePrefix+name+" has no reason; add one after --")
				}
				r := ignoreRule{file: f, types: types, reason: reason}
				switch {
				case name == ignoreStart:
					if open != nil {
						warnings = append(warnings, where+": "+directivePrefix+ignoreStart+" inside another range; the earlier range ends here")
						open.end = pos.Line - 1
						rules = append(rules, *open)
					}
					r.first = pos.Line
					open = &r
					continue
				case docOf[c] != nil:
					fd := docOf[c]
					r.first, r.end = pkg.Fset.Position(fd.Pos()).Line, pkg.Fset.Position(fd.End()).Line
				case codeBefore(f.Src, pos.Offset):
					r.first, r.end = pos.Line, pos.Line
				default:
					r.first, r.end = pos.Line+1, pos.Line+1
				}
				rules = append(rules, r)
			case criticalDirective:
				// Critical reads the critical region directive.
			case ignoreEnd:
				if open == nil {
					warnings = append(warnings, where+": "+directivePrefix+ignoreEnd+" without a matching "+directivePrefix+ignoreStart)
					continue
				}
				open.end = pos.Line
				rules = append(rules, *open)
				open = nil
			default:
				warnings = append(warnings, where+": unknown directive "+directivePrefix+name)
			}
		}
	}
	if open != nil {
		warnings = append(warnings, fmt.Sprintf("%s:%d: %s%s has no %s%s; it runs to the end of the file",
			f.Rel, open.first, directivePrefix, ignoreStart, directivePrefix, ignoreEnd))
		open.end = lastLine
		rules = append(rules, *open)
	}
	return rules, warnings, nil
}

// parseIgnoreArgs reads "[TYPE,...] [-- reason]" after the directive. Without mutator names, every mutator is stopped.
func parseIgnoreArgs(args string) ([]Type, string, error) {
	args = strings.TrimSpace(args)
	list, reason := args, ""
	if i := strings.Index(args, "--"); i >= 0 {
		list, reason = strings.TrimSpace(args[:i]), strings.TrimSpace(args[i+2:])
	}
	var types []Type
	for name := range strings.SplitSeq(list, ",") {
		if name = strings.TrimSpace(name); name == "" {
			continue
		}
		if !slices.Contains(Known, Type(name)) {
			return nil, "", fmt.Errorf("unknown mutator type %q in %s%s", name, directivePrefix, ignoreDirective)
		}
		types = append(types, Type(name))
	}
	return types, reason, nil
}

// codeBefore reports whether there is code on the same line before offset in src.
func codeBefore(src []byte, offset int) bool {
	start := bytes.LastIndexByte(src[:offset], '\n') + 1
	return len(bytes.TrimSpace(src[start:offset])) > 0
}
