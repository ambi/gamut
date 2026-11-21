# gamut design: selection and order

This document defines selecting the mutants to judge, the execution order, integration mode, judging on every save, shards and time budgets, and limiting the call depth.
The [design overview](README.md) sets out the overall purpose and roadmap.

## Selecting mutants to judge

### Diff (`-since`)

This mode tells how well the changed code is tested, for pull request checks and coding agents' iterations.

- The diff from `git merge-base <ref> HEAD` to the working tree comes from `git diff -M --no-prefix -U0`. Untracked files (except ignored ones) count as entirely new. Without `git`, or if the ref does not resolve, the run stops with exit code 1.
- The diff is in lines, but for Go files the lines around a change are split into tokens with `go/scanner` and matched by longest common subsequence, keeping only the ranges of changed tokens on the new side. Changes only to comments, whitespace, or line breaks make no range. Removed tokens make a zero-width range at that position on the new side.
- A mutant is selected when a changed range intersects the range of the site's expression (`-scope node`, the default) or of the enclosing declaration (`-scope decl`). A zero-width range intersects when it lies strictly inside the range (not at its ends). `decl` also catches changes to a declaration's signature.
- Mutants outside the diff are `PENDING` and not counted. Packages the diff does not touch (no files changed in the directories of the package and its reverse dependencies, and no mutant intersects the diff) are not built.
- Mutants outside the diff that were `KILLED` last time by a test that no longer exists are checked again. If they are not detected this time, the report's `gamut.previouslyKilledBy` names the previous killing test, and the summary lists them under "no longer detected". Without a cache the previous killing test is unknown, so they are not checked.
- With `-include-test-changes`, mutants outside the diff that were not `KILLED` last time and are reached by tests the diff changed are checked again too. The changed tests are the top-level tests that transitively refer to test-file declarations with changed ranges. If `TestMain` or `init` changed, every test counts. For tests of reverse dependencies, a changed test file means every test of that package.
- Renamed files are followed with `-M`, and cached verdicts under the old path's identifiers are carried to the new identifiers (`IDAt`). Coverage records are measured again, since the site keys change.
- Coverage recording follows the rules of [reusing coverage records](reuse.md#reusing-coverage-records) regardless of the diff, because checking mutants outside the diff again needs the current reach.

### Identifiers (`-mutant`)

- It takes an identifier or a unique prefix of one, and can be repeated. If a value matches none or several, the candidates are listed and the run stops with exit code 1. It cannot be combined with `-since` (exit code 1).
- Only the packages containing the named mutants are prepared. The report lists only the named mutants.
- Coverage recording measures again, besides the tests whose records cannot be restored by the rules, the tests that reached the named mutants' sites. A test added to fill a survivor changes its test key and is measured again.
- The named mutants run without using cached verdicts, and their verdicts are written back.

## Execution order

### Test order

Tests run in this order, stopping at the first failure:

1. The test that killed this mutant last time.
2. Tests that killed other mutants of the same site. The cache is written each time a run finishes, so this includes those detected earlier in the same run.
3. The remaining tests.

In a reused test process, the judging test calls them with `t.Run` in this order.
With `-isolate`, tests in one process run in declaration order regardless of the order in `-test.run`, so the order is set by splitting processes.
Each process holds the tests of one test binary only, the package's own test binary first.
The timeout is set per command or per process.
If the previous killing test is gone, step 1 is skipped.

### Mutant order

Mutants run in decreasing order of estimated time (the sum of the coverage-recording times of the tests to run, plus the package's build time for standalone mutants), the LPT rule.
This reduces the chance that one long mutant is left running alone at the end of the parallel run.
Compared with shortest first and with enumeration order, the differences were a few percent; shortest first had no advantage beyond progress appearing to move faster.

## Integration mode

Go builds a separate test binary per package, so behavior observable only in the tests of a calling package cannot be detected by the target package's tests alone.
`-integration` also tries detection with the tests of reverse dependencies.

- Reverse dependencies are found by running `go list -deps -test` over every package of the main module and following the test binaries' dependencies backwards: packages with tests whose test binaries link the target package. The target package itself is excluded.
- The reverse dependencies' test binaries are built with the same overlay that carries the target package's rewrite. The target package's helper file holds the mutant numbers, so the same `GAMUT_MUTANT` selects them. With two or more target packages, a reverse dependency's test binary is built separately per target package, because putting two rewrites into one binary makes the numbers, which start at 1 per package, collide.
- Coverage recording and running mutants span every test binary. Tests of reverse dependencies are named `<import path>.<test name>`; the package's own tests keep their names.
- A reverse dependency has one test key, made from its Go files, test files, embedded files, `testdata`, and the files of the local packages its test binary depends on (except the target package).
- A reverse dependency whose coverage records can all be restored takes its test list from the records and is not built until coverage must be measured or a mutant run. A package none of whose tests reaches the target package's sites keeps that record in the cache, so it is not built from then on.
- The self-check uses only the package's own test binary.
- For standalone mutants, the package's own test binary and the test binaries of reverse dependencies with tests that reached the site are built separately when their turn comes, and run. Mutants without reach records (`Untouched`) are judged with all of the package's own tests and all tests of the reverse dependencies that reached any site.
- If the baseline tests of a reverse dependency fail, the run also stops without judging, with exit code 4.

## Judging on every save

`-watch` keeps watching the module's `.go` files after judging and judges again on every change. It ends with `SIGINT` or `SIGTERM`.

- Without `-since`, the scope is `-since HEAD -include-test-changes`: the diff being edited, and the survivors that changed tests reach.
- The Go standard library has no file watching, so the modification times and sizes of `.go` files are compared every 500 milliseconds. Directories whose names start with `.` (`.git`, `.gamut`), `testdata`, and `vendor` are not watched.
- After a change is found, gamut waits 1 second before judging, folding files saved in a row into one run.
- The state is taken before judging and compared after it. Any number of saves during a run fold into the one run that follows.
- Failing baseline tests or a failed quality gate do not stop it; the exit code goes to stderr and it waits for the next save.

## Shards and time budgets

These split judging a whole large repository across several CI jobs and cut it off in time.

- `-shard k/n` judges only the mutants whose identifier's FNV-1a hash modulo n is k−1, and marks the rest `PENDING`. Splitting by identifier keeps the other mutants' assignments as mutants come and go, so each shard's cache stays effective. Each shard builds and records coverage in full.
- At the end of each run, the run's internal report is kept in the cache (the latest one).
- `gamut merge [-o <report>] [-cache-out <cache>] <shard cache>...` combines the reports kept in the caches and the caches' records. It never reads report files. For the same mutant it takes the verdict that is not `PENDING`, recounts, and recomputes the kill matrix. The report embeds the original sources, read from the module directory the shards judged if it exists here, and otherwise from the module of the working directory. Only caches of the same format version and environment are combined, and the records of unchanged reruns (whose key includes the shard) are not written. It prints the summary of the combined report and exits with code 2 if the quality gate is not met.
- `-time-budget <duration>` starts no new mutant once that much time has passed since gamut started, and marks the rest `PENDING`. Running mutants are not stopped. Mutants that never started are not written to the cache, so the next run reuses what was judged and continues from there. Runs with a budget skip the unchanged-rerun shortcut (the previous report does not include the rest).

## Limiting the call depth

With `-max-depth <n>`, coverage recording counts only reach where the call depth from the test function to the site is at most n.
For infrastructure functions that many tests pass through by accident, this reduces the reaching tests and makes it easier to see which test guards what.
It increases survivors and `NOT COVERED`, so it is off by default.

- The depth is counted inside the helpers `gamutHit` and `gamutInfect` by walking the frames of `runtime.Callers` and counting the frames of functions in the module up to one below the nearest `testing.tRunner` (the test function, or the subtest's function). The function containing the site counts as 1, and the test function itself is not counted. Subtests count from the subtest's function.
- Consecutive frames of functions of the same declaration count as one, because schemata may wrap a site in a function literal (`func() { ... }()`), which should not count as a level. Users' function literals and recursion are treated the same way. Helper functions (names starting with `gamut`) are not counted.
- Calls not under a test function (package initialization, goroutines started by tests) are not limited.
- Reach beyond the limit is recorded as "d" and counts as neither reach nor infection. If the same test also reaches the site shallowly, that counts as reach. Frames are walked only while recording coverage, so judging mutants is no slower.
- The depth limit changes the reach records, so it is part of the cache's environment. Sites reached only beyond the limit stay in the coverage records (`deep`), and runs that restore records still report the counts.
- The numbers of excluded tests and reaches appear in the report's `gamut.maxDepthExcluded` and in the summary.
- A smaller limit moves more mutants from killed to `NOT COVERED` and shortens the run, because fewer tests reach each mutant.

## Rejected ideas

| Idea | Why not |
| --- | --- |
| Set the test order by the order in `-test.run` | Tests in one process run in declaration order, so the order in the pattern changes nothing |
| In integration mode, put the rewrites of every target package into a reverse dependency's test binary | Mutant numbers start at 1 per package, so one `GAMUT_MUTANT` would enable mutants of several packages |
