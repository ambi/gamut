package run

import (
	"maps"
	"slices"

	"github.com/ambi/gamut/internal/cache"
)

// reuse writes the verdicts of the jobs whose cached verdicts can be reused, and returns the jobs that need running.
// Coverage records are redone every time, so reach and infection always come from the current source.
// Mutants not reached and not infected are never run anyway, so they do not use the cache.
//
// With matrix (kill matrix), a KILLED verdict is reused only when it ran every infected test and neither the tests to run nor their keys changed.
// LIVED already ran every test, so it is reused by the usual rules.
func reuse(jobs []job, store *cache.Store, dup map[string]bool, matrix bool) []job {
	var pending []job
	for _, j := range jobs {
		m := j.unit.Mutants[j.index]
		cur := j.entry()
		if cur == nil || dup[m.ID] || j.forced {
			pending = append(pending, j)
			continue
		}
		old := store.Get(m.ID)
		if !reusable(old, cur) || matrix && Status(old.Status) == Killed && (!old.Matrix || !sameTests(old, cur)) {
			pending = append(pending, j)
			continue
		}
		var tests []string
		if len(j.reached) > 0 {
			tests = testNames(j.reached)
		}
		j.unit.results[j.index] = &Result{
			Mutant: m, Status: Status(old.Status), Reason: old.Reason, Mode: old.Mode,
			Tests: tests, KilledBy: old.KilledBy, Killer: old.Killer, Cached: true,
			Matrix: old.Matrix || Status(old.Status) == Lived, Killers: old.Killers,
		}
	}
	return pending
}

// reusable reports whether the previous verdict old may be reused under the current keys cur.
//
//   - Every status requires the enclosing declaration and the judging mode to be unchanged.
//   - KILLED: the dependencies are unchanged, and the detecting (top-level) test is unchanged and still run. When the detecting test is unknown, the rule for LIVED applies.
//   - LIVED: the dependencies are unchanged, the sets of reaching tests and tests to run are the same, and none of them changed.
//   - TIMED OUT, and NOT VIABLE of standalone builds: the dependencies are unchanged.
func reusable(old, cur *cache.Entry) bool {
	if old == nil || old.Decl != cur.Decl || old.Mode != cur.Mode {
		return false
	}
	sameDeps := old.Deps == cur.Deps && old.Imports == cur.Imports
	switch Status(old.Status) {
	case TimedOut, NotViable:
		return sameDeps
	case Killed:
		if !sameDeps {
			return false
		}
		if old.Killer == "" {
			return sameTests(old, cur)
		}
		key, ok := old.TestKeys[old.Killer]
		return ok && slices.Contains(cur.Ran, old.Killer) && key == cur.TestKeys[old.Killer]
	case Lived:
		return sameDeps && sameTests(old, cur)
	}
	return false
}

func sameTests(old, cur *cache.Entry) bool {
	return slices.Equal(old.Reached, cur.Reached) && slices.Equal(old.Ran, cur.Ran) && maps.Equal(old.TestKeys, cur.TestKeys)
}

// entry returns a record with the job's current keys and no verdict, or nil when there are no keys.
func (j job) entry() *cache.Entry {
	keys := j.unit.Keys
	if keys == nil {
		return nil
	}
	m := j.unit.Mutants[j.index]
	e := &cache.Entry{
		Package: j.unit.Pkg.ImportPath, Mode: j.mode,
		Decl: keys.Decl(m), Deps: keys.Deps(m), Imports: keys.Imports(),
		Reached: sortedNames(j.reached), Ran: sortedNames(j.ran), TestKeys: map[string]string{},
	}
	for _, t := range slices.Concat(j.reached, j.ran) {
		if t.key == "" {
			// Verdicts involving a test without a key (from a reverse dependency without cache keys) are not recorded.
			return nil
		}
		e.TestKeys[t.name] = t.key
	}
	return e
}

// record writes the verdict of a run to the cache with the current keys.
func (j job) record(store *cache.Store, r *Result) {
	if store == nil {
		return
	}
	e := j.entry()
	if e == nil {
		return
	}
	e.Status, e.Reason, e.Mode, e.KilledBy, e.Killer = string(r.Status), r.Reason, r.Mode, r.KilledBy, r.Killer
	e.Matrix, e.Killers = r.Matrix, r.Killers
	store.Put(j.unit.Mutants[j.index].ID, e)
}

func sortedNames(ts []*testCase) []string {
	if len(ts) == 0 {
		return nil
	}
	names := testNames(ts)
	slices.Sort(names)
	return names
}

// duplicateIDs returns identifiers shared by two or more mutants. This happens with two init functions of the same shape, for example, and the cache cannot tell them apart.
func duplicateIDs(targets []Target) map[string]bool {
	count := map[string]int{}
	for _, t := range targets {
		for _, m := range t.Mutants {
			count[m.ID]++
		}
	}
	dup := map[string]bool{}
	for id, n := range count {
		if n > 1 {
			dup[id] = true
		}
	}
	return dup
}
