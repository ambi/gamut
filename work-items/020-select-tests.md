---
status: done
stage: 4
depends_on: [004]
created_at: 2026-11-20
---

# Select and print the tests a change may break

## Motivation

The previous verdicts and coverage records say which tests detected mutations of which functions, so the tests a change may break can be selected in under a second without running anything.

## Summary

- `gamut select-tests` is a mode of `judge`, sharing flag parsing, loading, key computation, and the cache environment, and branching to `selectTestsMain` before any test runs.
- `-format run|json` prints a `go test -run` regular expression per package, or the groups, counts, and reasons.
- Previous verdicts are looked up by file and declaration name (`cache.Store.Each`, `mutant.DeclName`), since rewriting a declaration may change identifiers; the cache is only read.
- Only test function names are selected from `Keys.ChangedTests`, which also returns changed helpers.
- Without a usable cache it exits with code 1.
- `TestSelectTests` (`testdata/selecttests`) asserts the order after a kill matrix run in both formats. Selecting after a one-function change on idmagic's `usecases` took 0.22 s.
