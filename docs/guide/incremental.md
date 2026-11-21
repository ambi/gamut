# gamut guide: incremental runs

This document covers reusing verdicts, selecting by diff and by mutant, integration mode, and selecting the tests a change may break.

## Reusing verdicts

Verdicts are saved in the cache. The next run does not run a mutant again when its enclosing declaration, the declarations of the same package it refers to, the local module's packages it depends on, the tests that reached it, and the environment (the Go version, gamut, `go.mod`, `go.sum`, the flags) are unchanged.
Coverage records are saved per test too, and only tests that entered a changed declaration, changed tests, and new tests are measured again.
Changes to comments or whitespace, and moved lines, do not cause a rerun.
Stopping with Ctrl-C keeps the verdicts reached so far.
The [design](../design/reuse.md#reusing-verdicts) defines the rules.

## Diffs, selecting mutants, and integration mode

```console
# Judge only the mutants at code changed since main. The rest become PENDING.
gamut -since main ./path/to/module/...
# After writing a test for a survivor, check just that mutant again.
gamut -mutant 'pkg/file.go::CONDITIONALS_BOUNDARY::IsAdult::1a2b' ./path/to/pkg
# Also try to detect mutants with the tests of calling packages.
gamut -integration ./path/to/pkg
```

`-since` also rechecks mutants outside the diff whose previous detecting test is gone, and reports those no longer detected with the previous detecting test in the report's `gamut.previouslyKilledBy`.
Under `-integration`, a test of a reverse dependency is named `<import path>.<test name>`.
Reverse dependencies where no test reaches a mutated site are not built from the next run on.
The [design](../design/selection.md#selecting-mutants-to-judge) and the [design](../design/selection.md#integration-mode) define the rules.

## Selecting the tests a change may break

`gamut select-tests` selects, from the previous verdicts and without running anything, the tests the diff may break.
It orders them as tests that detected mutants of changed declarations, tests that reached those declarations, then changed tests, and prints a `go test -run` regular expression per package (`-format json` also gives the reasons).
Run gamut beforehand with the same flags as for judging.

```console
gamut select-tests -since main ./path/to/pkg
```
