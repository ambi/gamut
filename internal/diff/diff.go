// Package diff finds, from git's diff, the token ranges changed in the files of the working tree.
// The diff comes per line, but for Go files the changed lines are split into tokens and matched before and after, so only changed tokens become ranges.
// Changes to comments and whitespace alone make no ranges.
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

// span is the byte range [Start, End) in a file. Start == End means tokens were removed at that position.
type span struct{ start, end int }

// Changes are the ranges changed on the new side of the diff (the working tree). Every path is absolute with symbolic links resolved.
type Changes struct {
	// Base is the commit on the old side of the diff.
	Base string
	// Renames maps the new paths of renamed files to their old paths.
	Renames map[string]string

	spans map[string][]span
	// paths are the files the diff touches (including removed files and files before renaming).
	paths map[string]bool
}

// ErrGit means the diff could not be obtained, for example because git is missing or the ref cannot be resolved.
var ErrGit = errors.New("cannot compute the diff")

// Since finds, in the repository containing dir, the diff from the merge-base of ref and HEAD to the working tree.
// Untracked files (except ignored ones) count as wholly new.
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

// resolve resolves symbolic links in path, returning it as is when it cannot.
// It lets paths match even when go list and git spell the same directory differently (such as /var and /private/var on macOS).
func resolve(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return filepath.Clean(path)
}

// Parse finds the ranges from patch, the output of git diff -U0 --no-prefix run in root, and untracked, the untracked files relative to root.
// read reads files of the working tree.
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
	// hunk pairs the old lines old with the new lines add. With -U0 there are no context lines.
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
			// There is no content diff, so the whole file counts as changed.
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
	// Also index by the path with symbolic links resolved, so it matches the callers' paths.
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

// hunkStart returns c of "@@ -a,b +c,d @@".
func hunkStart(line string) (int, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 || !strings.HasPrefix(fields[2], "+") {
		return 0, fmt.Errorf("malformed hunk header %q", line)
	}
	n, _, _ := strings.Cut(fields[2][1:], ",")
	return strconv.Atoi(n)
}

// lineStarts returns the byte offset of the start of each line, followed by len(src).
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

// hunk records the changed range starting at line start on the new side.
// Without new lines, lines count as removed after line start.
func (c *Changes) hunk(path string, src []byte, lines []int, start int, oldLines, newLines []string) {
	lineOffset := func(n int) int {
		// n is a 1-based line number; values outside the range are clamped.
		if n < 1 {
			return 0
		}
		if n > len(lines) {
			return len(src)
		}
		return lines[n-1]
	}
	if len(newLines) == 0 {
		// A deletion-only hunk of -U0 points after line start on the new side.
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
	// Token errors cannot be avoided in diff fragments (such as unclosed strings), so they are ignored.
	s.Init(f, src, func(token.Position, string) {}, 0)
	var out []tok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			return out
		}
		if t == token.SEMICOLON && lit == "\n" {
			// Semicolons inserted at newlines only reflect different line breaks.
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

// maxCells caps the size of the token matching table. Beyond it, every token on the new side counts as changed.
const maxCells = 4 << 20

// changedTokens finds the longest common subsequence of the old and new token lists, and returns the ranges of tokens changed on the new side.
// A token only on the old side makes a zero-width range at the next common token on the new side.
func changedTokens(oldSrc string, newSrc []byte) []span {
	a, b := tokens([]byte(oldSrc)), tokens(newSrc)
	if len(a)*len(b) > maxCells {
		if len(b) == 0 {
			return nil
		}
		return []span{{b[0].start, b[len(b)-1].end}}
	}
	same := func(x, y tok) bool { return x.tok == y.tok && x.lit == y.lit }
	// lcs[i][j] is the length of the longest common subsequence of a[i:] and b[j:].
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

// Intersects reports whether the range [start, end) of path intersects a changed range.
// A zero-width range (removed tokens) intersects when it lies strictly inside [start, end).
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

// TouchesTestFile reports whether the diff touches a test file (_test.go) directly in dir, including removed files and files before renaming.
func (c *Changes) TouchesTestFile(dir string) bool {
	d := resolve(dir)
	for p := range c.paths {
		if filepath.Dir(p) == d && strings.HasSuffix(p, "_test.go") {
			return true
		}
	}
	return false
}

// Files returns the number of files with changed ranges.
func (c *Changes) Files() int { return len(c.spans) }
