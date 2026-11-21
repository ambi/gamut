# gamut guide: running

This document covers choosing targets, the flags, test processes, critical regions and the quality gate, and exit codes.

## Choosing targets

```console
gamut -o report.json ./path/to/package
gamut -o report.json ./path/to/module/...
```

Each argument is a package directory, or `<directory>/...` for every package below it; you can give several.
gamut judges all the packages in one run and writes one report.
The target packages must belong to the same module.
Mutants of a package without test files are reported `NOT COVERED` without a build.
A package's mutants are judged only with that package's tests. The tests of other target packages link the unmutated code.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-workers` | Half the number of CPUs (at least 1) | The number of test processes running in parallel |
| `-only` | The default set | The mutators to enable (comma-separated). Mutators off by default can be chosen too |
| `-disable` | None | Mutators to remove from the default set (comma-separated). Cannot be combined with `-only` |
| `-list-mutators` | false | Print each mutator's name, description, example, and whether it is in the default set, then exit |
| `-version` | false | Print the gamut version and exit |
| `-arid-call` | None | Leave calls matching this glob (`<package>.<Func>`, `<package>.<Type>.<Method>`) unmutated, as logging is. Repeatable |
| `-arid-package` | None | Leave calls into this package and the packages below it unmutated. Repeatable |
| `-no-arid` | false | Also mutate code that tests rarely observe, such as logging (arid code) |
| `-exclude-files` | None | Do not mutate files whose module-relative path matches this regular expression. Repeatable |
| `-config` | `gamut.json` at the module root | The path of the config file |
| `-o` | None | Where to write the JSON report (mutation-testing-elements format) |
| `-reporter` | `text` (plus `json` with `-o`) | The report format: `text` (the summary on stdout), `json` (mutation-testing-elements format), `html`, `agent` (JSON for agents on stdout), or `simplify` (candidates for unneeded code on stdout). Repeatable |
| `-report-dir` | `.gamut/report` | Where to write `html`, and `json` without `-o` |
| `-annotations` | None | With `github`, print the survivors to fix as GitHub Actions annotations on stdout |
| `-dry-run` | false | Only list the mutants and their viability |
| `-timeout-factor` | 5 | The factor applied to the baseline time of the selected tests |
| `-timeout-min` | 2s | The time added to the timeout |
| `-q` | false | Do not print progress |
| `-tags` | None | Build tags for loading, building, and type-checking (comma-separated) |
| `-test-flags` | None | Flags passed to every run of a test binary (space-separated, such as `"-short"`) |
| `-cache` | `.gamut/cache.json` | Where to read and write the cache of verdicts and coverage records (relative to the working directory) |
| `-no-cache` | false | Neither read nor write the cache |
| `-checkpoint-interval` | 10s | Also write the cache at this interval during the run, so a hard kill loses at most this much of the verdicts (0 disables) |
| `-since` | None | Judge only the mutants intersecting the diff from the merge-base of this Git ref and `HEAD` to the working tree, and report the rest as `PENDING` |
| `-scope` | `node` | The range intersected with the diff under `-since`: `node` is the mutated expression, `decl` the enclosing declaration |
| `-include-test-changes` | false | Under `-since`, also recheck survivors outside the diff that tests changed by the diff reach |
| `-mutant` | None | Judge only the mutant with this identifier (or a unique prefix of it), ignoring the cache. Repeatable |
| `-integration` | false | Also try to detect mutants with the tests of module packages that import the target packages |
| `-watch` | false | After judging, keep watching `.go` files and judge the diff again on every save (the default range is `-since HEAD -include-test-changes`). Failures do not stop it |
| `-shard` | None | With `k/n`, judge only the k-th of n groups of mutants split by a hash of their identifiers. Combine the shards' caches with `gamut merge` |
| `-time-budget` | 0 | Once this much time has passed, start no new mutants and report the rest as `PENDING`. The next run with the same cache continues from there |
| `-kill-matrix` | false | Run every infected test of each mutant, record every test that detects it, and report tests that detect nothing, subsumed tests, and duplicate tests. Slow, as it starts a process per mutant |
| `-max-depth` | 0 | Count only reaches within this many calls of module functions from the test function as coverage (0: no limit). Survivors and `NOT COVERED` increase |
| `-isolate` | false | Start a test process per mutant instead of reusing processes across mutants |
| `-detect-equivalent` | false | Compile the package for each survivor and report it `EQUIVALENT` when its machine code equals the original's (nothing is written to the build cache) |
| `-critical` | None | Make files whose module-relative path matches this glob (`**` for any depth) a critical region. Repeatable |
| `-threshold-high` | 80 | Label the summary `[high]` when the score is at or above this |
| `-threshold-low` | 60 | Label it `[low]` when the score is at or above this, and `[danger]` below it |
| `-threshold-break` | 0 (disabled) | Exit with code 2 when the score is below this |
| `-fail-on-survived` | false | Exit with code 2 when any unaccepted mutant survives |
| `-cpuprofile` | None | Where to write a CPU profile of gamut itself (for developing gamut) |

## Test processes

Test processes are reused across mutants during one gamut run.
When gamut finds that a mutant has dirtied a process's state, it throws the process away and judges again in a new process.
Before judging, gamut checks with one test that mutations reach the tests (forced fail).
If they do not, it stops with exit code 5 without judging.
If a test fails with every mutation disabled, gamut shows that test's output and stops with exit code 4.

## Critical regions and the quality gate

Code such as authentication or money calculations can be made a critical region, where every mutant must be detected or accepted.
Files matching a `-critical` glob, and functions with `//gamut:critical` right before their declaration, are critical regions.
If a critical region has an unaccepted survivor, or the score (the report's `gamut.mutationScore`: detected mutants as a share of detected mutants plus survivors) is below `-threshold-break`, gamut writes the reports and the summary and exits with code 2.

| Exit code | Meaning |
| --- | --- |
| 0 | Judging finished and the quality gate passed |
| 1 | An error in the arguments, the configuration, loading, building, and so on |
| 2 | Judging finished but the quality gate failed |
| 4 | Tests failed without mutations, so nothing was judged |
| 5 | The forced fail self-check found that mutations do not reach the tests, so nothing was judged |
