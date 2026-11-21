package run

import (
	"math"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/diff"
	"github.com/ambi/gamut/internal/mutant"
)

// Selection narrows the mutants to judge. Either Mutants or Changes is used.
type Selection struct {
	// Mutants are the mutants selected by identifier. When not nil, only these are judged, without the cache.
	Mutants map[*mutant.Mutant]bool
	// Changes is the diff of --since. When not nil, only mutants intersecting the diff and mutants outside it that need rechecking are judged,
	// and the rest become PENDING.
	Changes *diff.Changes
	// DeclScope reports whether the diff is intersected with the enclosing declaration's range instead of the mutant's.
	DeclScope bool
	// IncludeTestChanges reports whether survivors outside the diff that tests changed by the diff reach are rechecked.
	IncludeTestChanges bool
}

// touches reports whether the package needs preparing.
// With a diff, that is when a mutant intersects the diff, or the diff touches test files of the package or a reverse dependency.
// Test file changes (including removing detecting tests) may require rechecking mutants outside the diff.
// With only source changes and no intersecting mutant, there is nothing to judge, so neither a build nor coverage recording happens.
func (s *Selection) touches(u *unit) bool {
	switch {
	case s == nil:
		return true
	case s.Mutants != nil:
		for _, m := range u.Mutants {
			if s.Mutants[m] {
				return true
			}
		}
		return false
	}
	if s.Changes.TouchesTestFile(u.Pkg.Dir) {
		return true
	}
	for _, r := range u.RevDeps {
		if s.Changes.TouchesTestFile(r.Dir) {
			return true
		}
	}
	for _, m := range u.Mutants {
		if s.direct(u, m) {
			return true
		}
	}
	return false
}

// direct reports whether m is judged regardless of coverage records.
func (s *Selection) direct(u *unit, m *mutant.Mutant) bool {
	switch {
	case s == nil:
		return true
	case s.Mutants != nil:
		return s.Mutants[m]
	}
	node := m.Site
	// The RETURN_ZERO site is just the body's {, but the mutation replaces the whole body, so changes to the function body select it.
	if b, ok := m.Site.(*mutant.BodySite); ok {
		node = b.Func.Body
	}
	if s.DeclScope {
		for _, d := range m.File.AST.Decls {
			if d.Pos() <= m.Site.Pos() && m.Site.End() <= d.End() {
				node = d
			}
		}
	}
	fset := u.Pkg.Fset
	return s.Changes.Intersects(m.File.Path, fset.Position(node.Pos()).Offset, fset.Position(node.End()).Offset)
}

// mayRecheck reports whether mutants outside the diff may be selected after coverage recording.
func (s *Selection) mayRecheck() bool {
	return s != nil && s.Changes != nil
}

// selects reports whether m is judged after coverage recording. tests are the tests that reached the mutation site.
// A mutant outside the diff is rechecked in the following cases. The second result is the previous detecting test's name when that test is gone.
//   - It was KILLED last time, and the detecting test no longer exists.
//   - With IncludeTestChanges, a test the diff changed reaches it, and it was not KILLED last time.
func (s *Selection) selects(u *unit, m *mutant.Mutant, tests []*testCase, store *cache.Store) (bool, string) {
	if s.direct(u, m) {
		return true, ""
	}
	if !s.mayRecheck() {
		return false, ""
	}
	var old *cache.Entry
	if store != nil {
		old = store.Get(m.ID)
	}
	if old != nil && Status(old.Status) == Killed && old.Killer != "" && u.test(old.Killer) == nil {
		return true, old.Killer
	}
	if s.IncludeTestChanges && (old == nil || Status(old.Status) != Killed) {
		for _, t := range tests {
			if u.changed[t.name] {
				return true, ""
			}
		}
	}
	return false, ""
}

// changedTests finds the names of the tests the diff changed. For the package's own tests it follows references between the test files' declarations;
// for reverse dependencies, a changed test file counts as a change to every test of that package.
func (s *Selection) changedTests(u *unit) map[string]bool {
	out := map[string]bool{}
	if s == nil || s.Changes == nil || !s.IncludeTestChanges {
		return out
	}
	if u.Keys != nil {
		for name := range u.Keys.ChangedTests(s.Changes.Intersects) {
			out[name] = true
		}
	}
	for _, b := range u.bins[1:] {
		touched := false
		for _, f := range b.revdep.TestFiles {
			if s.Changes.Intersects(f, 0, math.MaxInt) {
				touched = true
			}
		}
		if touched {
			for _, t := range b.tests {
				out[t.name] = true
			}
		}
	}
	return out
}
