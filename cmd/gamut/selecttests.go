package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/cache"
	"github.com/ambi/gamut/internal/diff"
	"github.com/ambi/gamut/internal/mutant"
	"github.com/ambi/gamut/internal/run"
)

// Groups of selected tests. A lower group is more likely to be broken by the change.
const (
	// groupKiller are tests that detected mutants of changed declarations.
	groupKiller = 1
	// groupReach are the other tests that reached changed declarations.
	groupReach = 2
	// groupChanged are changed tests and tests using changed test helpers.
	groupChanged = 3
)

// selectedTest is one selected test.
type selectedTest struct {
	Name    string `json:"name"`
	Group   int    `json:"group"`
	Kills   int    `json:"kills"`
	Reaches int    `json:"reaches"`
	Reason  string `json:"reason"`
}

// selectedPackage holds one package's selected tests in selection order.
type selectedPackage struct {
	Package string          `json:"package"`
	Tests   []*selectedTest `json:"tests"`
}

// selectTestsMain prints the tests the diff changes may break, selected from the previous verdicts and coverage records in the cache store. It runs nothing.
// Without a cache, or when it was discarded because the environment changed, it exits with code 1.
func selectTestsMain(stdout, stderr io.Writer, format string, targets []run.Target, changes *diff.Changes, store *cache.Store) int {
	switch {
	case store.Reset != "":
		fmt.Fprintf(stderr, "gamut: select-tests: the cache cannot be used because %s; run gamut with the same flags first\n", store.Reset)
		return exitError
	case store.Len() == 0:
		fmt.Fprintln(stderr, "gamut: select-tests: the cache has no verdicts; run gamut with the same flags first")
		return exitError
	}
	pkgs := selectTests(targets, changes, store)
	if format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(struct {
			Packages []*selectedPackage `json:"packages"`
		}{pkgs}); err != nil {
			fmt.Fprintln(stderr, "gamut:", err)
			return exitError
		}
		return exitOK
	}
	for _, p := range pkgs {
		names := make([]string, len(p.Tests))
		for i, t := range p.Tests {
			names[i] = regexp.QuoteMeta(t.Name)
		}
		fmt.Fprintf(stdout, "%s\t^(?:%s)$\n", p.Package, strings.Join(names, "|"))
	}
	return exitOK
}

// selectTests selects and orders tests per package:
//
//   - Group 1: tests that detected mutants of changed declarations, most detections first. Kill matrix verdicts count every detecting test, others only the first.
//   - Group 2: other tests that reached mutants of changed declarations, most reaches first. Declarations without previous verdicts (new ones, ones without mutants) select all the package's tests.
//   - Group 3: changed tests and tests that refer transitively to changed test helpers.
//
// Previous verdicts are looked up by file and declaration name rather than mutant identifier, because rewriting a declaration may change its mutants' identifiers.
func selectTests(targets []run.Target, changes *diff.Changes, store *cache.Store) []*selectedPackage {
	type declKey struct{ file, decl string }
	entries := map[declKey][]*cache.Entry{}
	store.Each(func(id string, e *cache.Entry) {
		parts := strings.Split(id, "::")
		if len(parts) == 4 {
			k := declKey{parts[0], parts[2]}
			entries[k] = append(entries[k], e)
		}
	})
	var out []*selectedPackage
	for _, t := range targets {
		byPkg := map[string]map[string]*selectedTest{}
		pick := func(name string) *selectedTest {
			pkg := t.Pkg.ImportPath
			// Tests of reverse dependencies are recorded as <import path>.<name>.
			if i := strings.LastIndex(name, "."); i >= 0 {
				pkg, name = name[:i], name[i+1:]
			}
			if byPkg[pkg] == nil {
				byPkg[pkg] = map[string]*selectedTest{}
			}
			s := byPkg[pkg][name]
			if s == nil {
				s = &selectedTest{Name: name}
				byPkg[pkg][name] = s
			}
			return s
		}
		var fresh []string
		for _, f := range t.Pkg.Files {
			for _, d := range f.AST.Decls {
				start, end := t.Pkg.Fset.Position(d.Pos()).Offset, t.Pkg.Fset.Position(d.End()).Offset
				if !changes.Intersects(f.Path, start, end) {
					continue
				}
				name := mutant.DeclName(d)
				old := entries[declKey{f.Rel, name}]
				if len(old) == 0 {
					fresh = append(fresh, name)
					continue
				}
				for _, e := range old {
					if run.Status(e.Status) == run.Killed {
						killers := e.Killers
						if !e.Matrix && e.Killer != "" {
							killers = []string{e.Killer}
						}
						for _, k := range killers {
							pick(k).Kills++
						}
					}
					for _, r := range e.Reached {
						pick(r).Reaches++
					}
				}
			}
		}
		for _, s := range byPkg {
			for _, sel := range s {
				switch {
				case sel.Kills > 0:
					sel.Group, sel.Reason = groupKiller, fmt.Sprintf("detected %d mutants in the changed declarations", sel.Kills)
				case sel.Reaches > 0:
					sel.Group, sel.Reason = groupReach, fmt.Sprintf("reached %d mutants in the changed declarations", sel.Reaches)
				}
			}
		}
		if len(fresh) > 0 && t.Keys != nil {
			for _, name := range t.Keys.TestFuncs() {
				if s := pick(name); s.Group == 0 {
					s.Group, s.Reason = groupReach, "the package has changed declarations without previous verdicts: "+strings.Join(fresh, ", ")
				}
			}
		}
		if t.Keys != nil {
			funcs := t.Keys.TestFuncs()
			for name := range t.Keys.ChangedTests(changes.Intersects) {
				// A changed helper itself is not a test, so it is not selected.
				if !slices.Contains(funcs, name) {
					continue
				}
				if s := pick(name); s.Group == 0 {
					s.Group, s.Reason = groupChanged, "the test or a test helper it uses changed"
				}
			}
		}
		for _, pkg := range slices.Sorted(maps.Keys(byPkg)) {
			var tests []*selectedTest
			for _, s := range byPkg[pkg] {
				if s.Group != 0 {
					tests = append(tests, s)
				}
			}
			if len(tests) == 0 {
				continue
			}
			slices.SortFunc(tests, func(a, b *selectedTest) int {
				return cmp.Or(cmp.Compare(a.Group, b.Group), cmp.Compare(b.Kills, a.Kills), cmp.Compare(b.Reaches, a.Reaches), strings.Compare(a.Name, b.Name))
			})
			out = append(out, &selectedPackage{Package: pkg, Tests: tests})
		}
	}
	return out
}
