package mutant

import (
	"bytes"
	"fmt"
	"go/ast"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/load"
)

// 無効化コメントの指令である。Go の指令コメントの慣習（//go:、//nolint:）に合わせ、// の直後に空白を置かない。
const (
	directivePrefix = "//gamut:"
	ignoreDirective = "ignore"
	ignoreStart     = "ignore-start"
	ignoreEnd       = "ignore-end"
	// criticalDirective は、関数を重要領域にする指令である。Critical が読む。
	criticalDirective = "critical"
)

// Ignored は、無効化コメントで止めた変異体と、コメントに書いた理由である。
type Ignored struct {
	Mutant *Mutant
	Reason string
}

// ignoreRule は、1 つの無効化コメントが止める行の範囲と変異演算子である。
type ignoreRule struct {
	file       *load.File
	first, end int // 止める行（両端を含む）
	types      []Type
	reason     string
}

// Ignores は、pkg の対象ファイルの無効化コメントを読み、ms を判定するもの kept と止めたもの ignored に分ける。
// kept の番号は 1 から振り直す。理由のないコメントや対応しない範囲は、警告として返して続ける。
// 未知の変異演算子の名前は誤りにする。綴りの誤りで、すべての演算子が止まったことに気づかなくなるためである。
//
// コメントが止める範囲は次のとおりである。
//   - //gamut:ignore [TYPE,...] [-- 理由]：同じ行にコードがあればその行、なければ次の行。関数の宣言の直前のコメント（Doc）なら、その関数全体。
//   - //gamut:ignore-start [TYPE,...] [-- 理由] から //gamut:ignore-end まで：その間の行。end がなければファイルの終わりまで。
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

// fileIgnores は、ファイル f の無効化コメントを読む。
func fileIgnores(pkg *load.Package, f *load.File) ([]ignoreRule, []string, error) {
	var rules []ignoreRule
	var warnings []string
	lastLine := pkg.Fset.Position(f.AST.FileEnd).Line
	// docOf は、関数の宣言の Doc にあるコメントから、その関数を引く。
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
				// 重要領域の指令は Critical が読む。
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

// parseIgnoreArgs は、指令の後ろの「[TYPE,...] [-- 理由]」を読む。演算子を省けば、すべての演算子を止める。
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

// codeBefore は、src の offset の前に、同じ行のコードがあるかを返す。
func codeBefore(src []byte, offset int) bool {
	start := bytes.LastIndexByte(src[:offset], '\n') + 1
	return len(bytes.TrimSpace(src[start:offset])) > 0
}
