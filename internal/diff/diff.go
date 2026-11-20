// Package diff は、git の差分から、作業ツリーのファイルの中で変わった字句の区間を求める。
// 差分は行の単位で得るが、Go のファイルは変わった行を字句に分けて前後で突き合わせ、変わった字句だけを区間にする。
// コメントと空白だけの変更は区間を作らない。
package diff

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// span は、ファイルの中のバイトの区間 [Start, End) である。Start == End は、その位置で字句が消えたことを表す。
type span struct{ start, end int }

// Changes は、差分の新しい側（作業ツリー）で変わった区間である。パスはどれも、シンボリックリンクを解決した絶対パスである。
type Changes struct {
	// Base は、差分の古い側のコミットである。
	Base string
	// Renames は、名前を変えたファイルの新しいパスから古いパスへの対応である。
	Renames map[string]string

	spans map[string][]span
	// paths は、差分が触れたファイル（消えたファイルと名前を変える前のファイルを含む）である。
	paths map[string]bool
}

// ErrGit は、git がないか、参照を解決できないなど、差分を得られなかったことを表す。
var ErrGit = errors.New("cannot compute the diff")

// Since は、dir を含むリポジトリで、ref と HEAD の merge-base から作業ツリーまでの差分を求める。
// 追跡していないファイル（無視するものを除く）は、全体が新しいとみなす。
func Since(dir, ref string) (*Changes, error) {
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	root := resolve(strings.TrimSpace(string(top)))
	base, err := git(root, "merge-base", ref, "HEAD")
	if err != nil {
		return nil, err
	}
	c := &Changes{Base: strings.TrimSpace(string(base))}
	patch, err := git(root, "-c", "core.quotePath=off", "diff", "-M", "--no-prefix", "--no-color", "--no-ext-diff", "-U0", c.Base, "--")
	if err != nil {
		return nil, err
	}
	untracked, err := git(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	var names []string
	for name := range strings.SplitSeq(string(untracked), "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	parsed, err := Parse(root, patch, names, os.ReadFile)
	if err != nil {
		return nil, err
	}
	parsed.Base = c.Base
	return parsed, nil
}

func git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: git %s in %s: %w: %s", ErrGit, strings.Join(args, " "), dir, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// resolve は、パスのシンボリックリンクを解決する。解決できなければそのまま返す。
// go list と git が同じディレクトリを別の綴り（macOS の /var と /private/var など）で返しても突き合わせられるようにする。
func resolve(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return filepath.Clean(path)
}

// Parse は、root で実行した git diff -U0 --no-prefix の出力 patch と、追跡していないファイル untracked（root からの相対パス）から区間を求める。
// read は作業ツリーのファイルを読む。
func Parse(root string, patch []byte, untracked []string, read func(string) ([]byte, error)) (*Changes, error) {
	c := &Changes{Renames: map[string]string{}, spans: map[string][]span{}, paths: map[string]bool{}}
	abs := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	for _, rel := range untracked {
		path := abs(rel)
		data, err := read(path)
		if err != nil {
			return nil, err
		}
		c.add(path, span{0, len(data)})
	}

	var (
		oldPath, newPath string
		src              []byte
		lines            []int
		inFile           bool
	)
	// hunk は、古い側の行 old と新しい側の行 add の組である。-U0 なので文脈の行を含まない。
	var hunkNewStart int
	var oldLines, newLines []string
	flush := func() {
		if newPath == "" || src == nil || len(oldLines)+len(newLines) == 0 {
			oldLines, newLines = nil, nil
			return
		}
		c.hunk(newPath, src, lines, hunkNewStart, oldLines, newLines)
		oldLines, newLines = nil, nil
	}
	sc := bufio.NewScanner(bytes.NewReader(patch))
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			oldPath, newPath, src, lines, inFile = "", "", nil, nil, true
		case !inFile:
			continue
		case strings.HasPrefix(line, "rename from "):
			oldPath = abs(unquote(strings.TrimPrefix(line, "rename from ")))
		case strings.HasPrefix(line, "rename to "):
			newPath = abs(unquote(strings.TrimPrefix(line, "rename to ")))
			c.Renames[newPath] = oldPath
			c.touch(oldPath)
			c.touch(newPath)
		case strings.HasPrefix(line, "--- "):
			if p := strings.TrimPrefix(line, "--- "); p != "/dev/null" {
				oldPath = abs(unquote(p))
				c.touch(oldPath)
			}
		case strings.HasPrefix(line, "+++ "):
			p := strings.TrimPrefix(line, "+++ ")
			if p == "/dev/null" {
				newPath = ""
				continue
			}
			newPath = abs(unquote(p))
			c.touch(newPath)
			data, err := read(newPath)
			if err != nil {
				return nil, err
			}
			src, lines = data, lineStarts(data)
		case strings.HasPrefix(line, "Binary files "):
			// 中身の差分がないので、ファイル全体を変わったとみなす。
			if newPath != "" {
				if data, err := read(newPath); err == nil {
					c.add(newPath, span{0, len(data)})
				}
			}
		case strings.HasPrefix(line, "@@ "):
			flush()
			start, err := hunkStart(line)
			if err != nil {
				return nil, err
			}
			hunkNewStart = start
		case strings.HasPrefix(line, "-"):
			oldLines = append(oldLines, line[1:])
		case strings.HasPrefix(line, "+"):
			newLines = append(newLines, line[1:])
		}
	}
	flush()
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for path := range c.spans {
		c.touch(path)
	}
	// 呼び出し側のパスと突き合わせられるよう、シンボリックリンクを解決したパスでも引けるようにする。
	c.normalize()
	return c, nil
}

func (c *Changes) touch(path string) {
	c.paths[path] = true
}

func (c *Changes) normalize() {
	spans := map[string][]span{}
	for p, s := range c.spans {
		spans[resolve(p)] = append(spans[resolve(p)], s...)
	}
	c.spans = spans
	paths := map[string]bool{}
	for p := range c.paths {
		paths[filepath.Join(resolve(filepath.Dir(p)), filepath.Base(p))] = true
	}
	c.paths = paths
	renames := map[string]string{}
	for n, o := range c.Renames {
		renames[resolve(n)] = filepath.Join(resolve(filepath.Dir(o)), filepath.Base(o))
	}
	c.Renames = renames
}

func unquote(p string) string {
	if strings.HasPrefix(p, `"`) {
		if u, err := strconv.Unquote(p); err == nil {
			return u
		}
	}
	return p
}

// hunkStart は、"@@ -a,b +c,d @@" の c を返す。
func hunkStart(line string) (int, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 || !strings.HasPrefix(fields[2], "+") {
		return 0, fmt.Errorf("malformed hunk header %q", line)
	}
	n, _, _ := strings.Cut(fields[2][1:], ",")
	return strconv.Atoi(n)
}

// lineStarts は、各行の先頭のバイト位置を返す。末尾に len(src) を足す。
func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	if starts[len(starts)-1] != len(src) {
		starts = append(starts, len(src))
	}
	return starts
}

func (c *Changes) add(path string, s span) {
	c.spans[path] = append(c.spans[path], s)
}

// hunk は、新しい側の start 行目から始まる変更の区間を記録する。
// 新しい行がなければ、start 行目の後ろで行が消えたとみなす。
func (c *Changes) hunk(path string, src []byte, lines []int, start int, oldLines, newLines []string) {
	lineOffset := func(n int) int {
		// n は 1 から始まる行番号である。範囲の外は端に寄せる。
		if n < 1 {
			return 0
		}
		if n > len(lines) {
			return len(src)
		}
		return lines[n-1]
	}
	if len(newLines) == 0 {
		// -U0 の削除だけの塊は、新しい側の start 行目の後ろを指す。
		at := lineOffset(start + 1)
		c.add(path, span{at, at})
		return
	}
	lo := lineOffset(start)
	hi := min(lineOffset(start+len(newLines)), len(src))
	if !strings.HasSuffix(path, ".go") {
		c.add(path, span{lo, hi})
		return
	}
	for _, s := range changedTokens(strings.Join(oldLines, "\n"), src[lo:hi]) {
		c.add(path, span{lo + s.start, lo + s.end})
	}
}

type tok struct {
	tok        token.Token
	lit        string
	start, end int
}

func tokens(src []byte) []tok {
	fset := token.NewFileSet()
	f := fset.AddFile("", -1, len(src))
	var s scanner.Scanner
	// 字句の誤りは差分の断片では避けられない（閉じていない文字列など）ので無視する。
	s.Init(f, src, func(token.Position, string) {}, 0)
	var out []tok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			return out
		}
		if t == token.SEMICOLON && lit == "\n" {
			// 改行から補われたセミコロンは、行の分け方の違いでしかない。
			continue
		}
		start := f.Offset(pos)
		end := start + len(lit)
		if lit == "" {
			end = start + len(t.String())
		}
		out = append(out, tok{tok: t, lit: lit, start: start, end: min(end, len(src))})
	}
}

// maxCells は、字句の突き合わせの表の大きさの上限である。超えたら新しい側の字句をすべて変わったとみなす。
const maxCells = 4 << 20

// changedTokens は、古い字句の列と新しい字句の列の最長共通部分列を求め、新しい側で変わった字句の区間を返す。
// 古い側にだけある字句は、新しい側の次の共通の字句の位置に幅 0 の区間を作る。
func changedTokens(oldSrc string, newSrc []byte) []span {
	a, b := tokens([]byte(oldSrc)), tokens(newSrc)
	if len(a)*len(b) > maxCells {
		if len(b) == 0 {
			return nil
		}
		return []span{{b[0].start, b[len(b)-1].end}}
	}
	same := func(x, y tok) bool { return x.tok == y.tok && x.lit == y.lit }
	// lcs[i][j] は a[i:] と b[j:] の最長共通部分列の長さである。
	lcs := make([][]int32, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(b)+1)
	}
	for i, x := range slices.Backward(a) {
		for j, y := range slices.Backward(b) {
			if same(x, y) {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []span
	i, j := 0, 0
	pointAt := func(j int) int {
		if j < len(b) {
			return b[j].start
		}
		return len(newSrc)
	}
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && same(a[i], b[j]):
			i++
			j++
		case j < len(b) && (i == len(a) || lcs[i][j+1] >= lcs[i+1][j]):
			out = append(out, span{b[j].start, b[j].end})
			j++
		default:
			p := pointAt(j)
			out = append(out, span{p, p})
			i++
		}
	}
	return out
}

// Intersects は、path の区間 [start, end) が変わった区間と交わるかを返す。
// 幅 0 の区間（消えた字句）は、[start, end) の内側（端を除く）にあれば交わるとみなす。
func (c *Changes) Intersects(path string, start, end int) bool {
	for _, s := range c.spans[resolve(path)] {
		if s.start == s.end {
			if start < s.start && s.start < end {
				return true
			}
			continue
		}
		if s.start < end && start < s.end {
			return true
		}
	}
	return false
}

// TouchesTestFile は、差分が dir の直下のテストファイル（_test.go）に触れるかを返す。消えたファイルと名前を変える前のファイルも含む。
func (c *Changes) TouchesTestFile(dir string) bool {
	d := resolve(dir)
	for p := range c.paths {
		if filepath.Dir(p) == d && strings.HasSuffix(p, "_test.go") {
			return true
		}
	}
	return false
}

// Files は、変わった区間を持つファイルの数を返す。
func (c *Changes) Files() int { return len(c.spans) }
