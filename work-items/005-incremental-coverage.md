---
status: done
stage: 2
depends_on: [004]
created_at: 2026-11-20
---

# Redo coverage recording only for affected tests

## Motivation

After [004](004-result-cache.md), coverage recording still ran every test one by one; on a package of idmagic with 135 tests this alone took about 7 s, most of an unchanged rerun.

## Summary

- Coverage records are cached per pair of target package and test binary, per top-level test. Sites use the position-independent `SiteKey`, mutants the hash part of their identifiers.
- Rather than estimating reach statically by name, the rewrite inserts `gamutEnter` at the start of each function body and records entered functions at run time. Static estimates over-remeasure on same-named identifiers and miss declarations called from other packages (such as `sort.Sort`).
- A declaration's reach key covers the declaration and its non-function dependencies only; called functions are caught by their own records.
- Changes to package variables, `init`, generated files, external dependencies, or rewrite levels remeasure every test.
- `internal/run/incremental_test.go` asserts the measured tests and the match with uncached verdicts across edits. On idmagic's `usecases`, an unchanged rerun went from 10.4 s to 2.9 s, and a one-function change took 5.9 s, measuring only the 28 tests that reached it.
