package run

import (
	"maps"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/cache"
)

// reuse は、キャッシュの判定を再利用できる作業の判定を書き、実行が要る作業を返す。
// 被覆の記録は毎回やり直すので、到達と感染はいつも今のソースのものである。
// 到達しない変異体と感染しない変異体はもともと実行しないので、キャッシュを使わない。
func reuse(jobs []job, store *cache.Store, dup map[string]bool) []job {
	var pending []job
	for _, j := range jobs {
		m := j.unit.Mutants[j.index]
		cur := j.entry()
		if cur == nil || dup[m.ID] {
			pending = append(pending, j)
			continue
		}
		old := store.Get(m.ID)
		if !reusable(old, cur) {
			pending = append(pending, j)
			continue
		}
		var tests []string
		if len(j.reached) > 0 {
			tests = testNames(j.reached)
		}
		j.unit.results[j.index] = &Result{
			Mutant: m, Status: Status(old.Status), Reason: old.Reason, Mode: old.Mode,
			Tests: tests, KilledBy: old.KilledBy, Cached: true,
		}
	}
	return pending
}

// reusable は、今の鍵 cur の下で、前の判定 old を再利用してよいかを返す。
//
//   - どの状態も、囲む宣言と判定の方式が変わらないことを求める。
//   - KILLED：依存が変わらず、検出したテストが変わらずに今も実行される。検出したテストが分からなければ LIVED と同じ規則にする。
//   - LIVED：依存が変わらず、到達したテストと実行するテストの集合が同じで、そのどれも変わらない。
//   - TIMED OUT と個別ビルドの NOT VIABLE：依存が変わらない。
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
		if old.KilledBy == "" {
			return sameTests(old, cur)
		}
		// 失敗の行はサブテストの名前を含みうるが、鍵はトップレベルのテストにある。
		killer, _, _ := strings.Cut(old.KilledBy, "/")
		key, ok := old.TestKeys[killer]
		return ok && slices.Contains(cur.Ran, killer) && key == cur.TestKeys[killer]
	case Lived:
		return sameDeps && sameTests(old, cur)
	}
	return false
}

func sameTests(old, cur *cache.Entry) bool {
	return slices.Equal(old.Reached, cur.Reached) && slices.Equal(old.Ran, cur.Ran) && maps.Equal(old.TestKeys, cur.TestKeys)
}

// entry は、作業の今の鍵を持ち、判定を持たない記録を返す。鍵がなければ nil を返す。
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
	for _, name := range slices.Concat(e.Reached, e.Ran) {
		e.TestKeys[name] = keys.Test(name)
	}
	return e
}

// record は、実行した判定を今の鍵とともにキャッシュへ書く。
func (j job) record(store *cache.Store, r *Result) {
	if store == nil {
		return
	}
	e := j.entry()
	if e == nil {
		return
	}
	e.Status, e.Reason, e.Mode, e.KilledBy = string(r.Status), r.Reason, r.Mode, r.KilledBy
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

// duplicateIDs は、2 体以上の変異体が持つ識別子を返す。同じ形の init が 2 つあるときなどに起こり、キャッシュで区別できない。
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
