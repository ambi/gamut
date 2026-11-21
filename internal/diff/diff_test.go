package diff_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/diff"
)

// repo creates a temporary Git repository with files committed once.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		write(t, dir, name, src)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "base"},
	} {
		gitRun(t, dir, args...)
	}
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name, src string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

// at returns the range of needle in src.
func at(t *testing.T, src, needle string) (int, int) {
	t.Helper()
	i := strings.Index(src, needle)
	if i < 0 {
		t.Fatalf("%q not in source", needle)
	}
	return i, i + len(needle)
}

func TestSince_MarksChangedTokensNotWholeLines(t *testing.T) {
	const before = "package p\n\nfunc F(a, b, c, d int) int {\n\tif a < b { return c + d }\n\treturn 0\n}\n"
	const after = "package p\n\nfunc F(a, b, c, d int) int {\n\tif a < b { return c * d }\n\treturn 0\n}\n"
	dir := repo(t, map[string]string{"p.go": before})
	write(t, dir, "p.go", after)
	c, err := diff.Since(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "p.go")
	cases := []struct {
		needle string
		want   bool
	}{
		{"c * d", true},
		// Unchanged tokens do not intersect, even on the same line.
		{"a < b", false},
		{"return 0", false},
	}
	for _, tc := range cases {
		start, end := at(t, after, tc.needle)
		if got := c.Intersects(path, start, end); got != tc.want {
			t.Errorf("Intersects(%q) = %t, want %t", tc.needle, got, tc.want)
		}
	}
	// A source-only change does not touch test files.
	if c.TouchesTestFile(dir) {
		t.Errorf("TouchesTestFile(%s) = true for a source-only change", dir)
	}
}

func TestSince_IgnoresCommentsAndWhitespace(t *testing.T) {
	const before = "package p\n\nfunc F(a int) int {\n\treturn a + 1\n}\n"
	const after = "package p\n\n// F adds 1 to a.\nfunc F(a int) int {\n\t// Add.\n\treturn a +\n\t\t1\n}\n"
	dir := repo(t, map[string]string{"p.go": before})
	write(t, dir, "p.go", after)
	c, err := diff.Since(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if c.Intersects(filepath.Join(dir, "p.go"), 0, len(after)) {
		t.Errorf("comment and line-break changes produced a changed span")
	}
}

func TestSince_DeletionMarksThePointInsideTheEnclosingRange(t *testing.T) {
	const before = "package p\n\nfunc F(a int) int {\n\ta++\n\treturn a\n}\n\nfunc G() {}\n"
	const after = "package p\n\nfunc F(a int) int {\n\treturn a\n}\n\nfunc G() {}\n"
	dir := repo(t, map[string]string{"p.go": before})
	write(t, dir, "p.go", after)
	c, err := diff.Since(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "p.go")
	fStart, _ := at(t, after, "func F")
	_, fEnd := at(t, after, "return a\n}")
	if !c.Intersects(path, fStart, fEnd) {
		t.Errorf("the function that lost a statement does not intersect the deletion")
	}
	gStart, gEnd := at(t, after, "func G() {}")
	if c.Intersects(path, gStart, gEnd) {
		t.Errorf("an untouched function intersects the deletion")
	}
}

func TestSince_UntrackedFilesAndRenames(t *testing.T) {
	const src = "package p\n\nfunc F(a int) int {\n\treturn a + 1\n}\n"
	dir := repo(t, map[string]string{"old.go": src, "sub/keep.go": "package sub\n"})
	gitRun(t, dir, "mv", "old.go", "renamed.go")
	write(t, dir, "sub/new.go", "package sub\n\nvar X = 1 + 2\n")
	c, err := diff.Since(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(dir, "renamed.go")
	if got := c.Renames[resolve(t, renamed)]; filepath.Base(got) != "old.go" {
		t.Errorf("Renames[renamed.go] = %q, want .../old.go (all: %v)", got, c.Renames)
	}
	// A rename without content changes makes no ranges.
	if c.Intersects(renamed, 0, len(src)) {
		t.Errorf("a pure rename produced a changed span")
	}
	if !c.Intersects(filepath.Join(dir, "sub", "new.go"), 0, 10) {
		t.Errorf("an untracked file is not treated as entirely new")
	}
	if c.TouchesTestFile(filepath.Join(dir, "sub")) {
		t.Errorf("TouchesTestFile(sub) = true without a test file")
	}
}

func TestSince_TouchesTestFileIncludesDeletedTests(t *testing.T) {
	dir := repo(t, map[string]string{"p.go": "package p\n", "p_test.go": "package p\n\nimport \"testing\"\n\nfunc TestP(t *testing.T) {}\n"})
	if err := os.Remove(filepath.Join(dir, "p_test.go")); err != nil {
		t.Fatal(err)
	}
	c, err := diff.Since(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !c.TouchesTestFile(dir) {
		t.Errorf("TouchesTestFile = false after deleting a test file")
	}
}

func TestSince_UnknownRefFails(t *testing.T) {
	dir := repo(t, map[string]string{"p.go": "package p\n"})
	if _, err := diff.Since(dir, "no-such-ref"); err == nil {
		t.Fatal("Since(no-such-ref) succeeded, want an error")
	}
}

func resolve(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
