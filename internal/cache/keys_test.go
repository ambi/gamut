package cache_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/load"
	"github.com/ambi/gamut/internal/mutant"
)

const baseSource = `package k

type Shape interface{ Area() int }

type Sq struct{ s int }

func (q Sq) Area() int { return q.s * q.s }

func Total(xs []Shape) int {
	t := 0
	for _, x := range xs {
		t = t + x.Area()
	}
	return t
}

func H(v int) int { return v + 1 }

func F(v int) int { return H(v) * 2 }

func G(v int) int { return v - step }

const step = 1
`

const baseTest = `package k

import "testing"

func helper(v int) int { return v }

func TestF(t *testing.T) { _ = F(helper(1)) }

func TestG(t *testing.T) { _ = G(1) }
`

// snapshot は、宣言ごとの Decl、Deps、Reach、テストごとの鍵、初期化の鍵である。
type snapshot struct {
	decl, deps, reach map[string]string
	tests             map[string]string
	init              string
	keys              *cache.Keys
}

func TestKeys_ChangeOnlyWithTheSourcesTheyCover(t *testing.T) {
	cases := []struct {
		name       string
		file       string
		old, new   string
		declDiffs  []string // Decl が変わる宣言
		depsDiffs  []string // Deps が変わる宣言
		reachDiffs []string // Reach が変わる宣言
		testDiffs  []string // 鍵が変わるテスト
		initDiff   bool     // Init が変わるか
		changed    []string // 変わった区間から ChangedTests が返すテスト
	}{
		{name: "comments and blank lines", file: "k.go", old: "func F(", new: "// F の注釈。\n\n\nfunc F("},
		{name: "new unrelated declaration", file: "k.go", old: "func G(", new: "func Unused() int { return 0 }\n\nfunc G("},
		{name: "reordered declarations", file: "k.go",
			old: "func F(v int) int { return H(v) * 2 }\n\nfunc G(v int) int { return v - step }\n",
			new: "func G(v int) int { return v - step }\n\nfunc F(v int) int { return H(v) * 2 }\n"},
		// 被覆の記録は入った関数をすべて記録するので、Reach は呼ぶ関数を含めない。
		{name: "helper body", file: "k.go", old: "return v + 1", new: "return v + 2",
			declDiffs: []string{"H"}, depsDiffs: []string{"F"}, reachDiffs: []string{"H"}},
		{name: "method reached through an interface", file: "k.go", old: "return q.s * q.s", new: "return q.s * 2",
			declDiffs: []string{"Sq.Area"}, depsDiffs: []string{"Total"}, reachDiffs: []string{"Sq.Area"}},
		{name: "a constant a function uses", file: "k.go", old: "const step = 1", new: "const step = 2",
			depsDiffs: []string{"G"}, reachDiffs: []string{"G"}},
		{name: "init runs before every declaration", file: "k.go", old: "func G(", new: "func init() { _ = 1 }\n\nfunc G(",
			depsDiffs: []string{"F", "G", "H", "Sq.Area", "Total"}, initDiff: true},
		{name: "package variable", file: "k.go", old: "func G(", new: "var scale = 3\n\nfunc G(", initDiff: true},
		{name: "test helper", file: "k_test.go", old: "return v }", new: "return v + 0 }", testDiffs: []string{"TestF"}, changed: []string{"TestF"}},
		{name: "test body", file: "k_test.go", old: "_ = G(1)", new: "_ = G(2)", testDiffs: []string{"TestG"}, changed: []string{"TestG"}},
		{name: "TestMain runs before every test", file: "k_test.go", old: "func helper(",
			new: "func TestMain(m *testing.M) { m.Run() }\n\nfunc helper(", testDiffs: []string{"TestF", "TestG"}, changed: []string{"TestF", "TestG"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "go.mod", "module k\n\ngo 1.27.4\n")
			write(t, dir, "k.go", baseSource)
			write(t, dir, "k_test.go", baseTest)
			before := take(t, dir)
			path := filepath.Join(dir, c.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), c.old) {
				t.Fatalf("%s does not contain %q", c.file, c.old)
			}
			edited := strings.Replace(string(data), c.old, c.new, 1)
			write(t, dir, c.file, edited)
			after := take(t, dir)
			compare(t, "Decl", before.decl, after.decl, c.declDiffs)
			compare(t, "Deps", before.deps, after.deps, c.depsDiffs)
			compare(t, "Reach", before.reach, after.reach, c.reachDiffs)
			compare(t, "Test", before.tests, after.tests, c.testDiffs)
			if got := before.init != after.init; got != c.initDiff {
				t.Errorf("Init changed = %t, want %t", got, c.initDiff)
			}
			// 書き換えた字句の区間を差分とみなす。
			start := strings.Index(edited, c.new)
			changed := after.keys.ChangedTests(func(p string, s, e int) bool {
				return p == path && s < start+len(c.new) && start < e
			})
			var got []string
			for _, name := range []string{"TestF", "TestG"} {
				if changed[name] {
					got = append(got, name)
				}
			}
			if strings.Join(got, " ") != strings.Join(c.changed, " ") {
				t.Errorf("ChangedTests = %v, want %v", got, c.changed)
			}
		})
	}
}

func take(t *testing.T, dir string) snapshot {
	t.Helper()
	pkgs, err := load.Load([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cache.NewKeys(pkgs[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	s := snapshot{decl: map[string]string{}, deps: map[string]string{}, reach: map[string]string{}, tests: map[string]string{},
		init: keys.Init(), keys: keys}
	for _, m := range mutant.Enumerate(pkgs[0], mutant.AllTypes) {
		s.decl[m.Scope] = keys.Decl(m)
		s.deps[m.Scope] = keys.Deps(m)
		s.reach[m.Scope] = keys.Reach(m)
	}
	for _, name := range []string{"TestF", "TestG"} {
		s.tests[name] = keys.Test(name)
	}
	return s
}

func compare(t *testing.T, what string, before, after map[string]string, changed []string) {
	t.Helper()
	for name, b := range before {
		want := false
		for _, c := range changed {
			want = want || c == name
		}
		if got := b != after[name]; got != want {
			t.Errorf("%s(%s) changed = %t, want %t", what, name, got, want)
		}
	}
}

func write(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStore_KeepsOtherPackagesAndDropsVanishedMutants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "cache.json")
	s, err := cache.Open(path, "env1")
	if err != nil {
		t.Fatal(err)
	}
	s.Put("a1", &cache.Entry{Package: "m/a", Status: "KILLED"})
	s.Put("a2", &cache.Entry{Package: "m/a", Status: "LIVED"})
	s.Put("b1", &cache.Entry{Package: "m/b", Status: "KILLED"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	s, err = cache.Open(path, "env1")
	if err != nil {
		t.Fatal(err)
	}
	if s.Reset != "" || s.Len() != 3 {
		t.Fatalf("reopened with Reset %q and %d entries, want 3 entries", s.Reset, s.Len())
	}
	// a2 は消えた変異体である。m/b は今回判定しないので残す。
	s.Prune([]string{"m/a"}, func(id string) bool { return id == "a1" })
	if s.Get("a1") == nil || s.Get("a2") != nil || s.Get("b1") == nil {
		t.Errorf("after Prune: a1 %v, a2 %v, b1 %v; want a1 and b1 only", s.Get("a1"), s.Get("a2"), s.Get("b1"))
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("want only the cache file left in its directory, got %v", entries)
	}

	s, err = cache.Open(path, "env2")
	if err != nil {
		t.Fatal(err)
	}
	if s.Reset == "" || s.Len() != 0 {
		t.Errorf("a changed environment must discard every entry, got Reset %q and %d entries", s.Reset, s.Len())
	}

	write(t, filepath.Dir(path), "cache.json", "{not json")
	s, err = cache.Open(path, "env1")
	if err != nil || s.Reset == "" || s.Len() != 0 {
		t.Errorf("a broken file must yield an empty store, got err %v, Reset %q, %d entries", err, s.Reset, s.Len())
	}
}
