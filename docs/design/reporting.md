# gamut design: verdicts and reports

This document defines the statuses, report formats, exit codes and the quality gate, the config file, selecting the tests a change may break, and the kill matrix.
The [design overview](README.md) sets out the overall purpose and roadmap.

## Verdicts and reports

| Status | Meaning |
| --- | --- |
| `KILLED` | One of the chosen tests failed |
| `LIVED` | It survived. The reason is `not infected` (reached, but the value did not change) or `not detected` (the value changed, but nothing failed) |
| `NOT COVERED` | No test reached it |
| `NOT VIABLE` | The mutated package fails to type-check |
| `TIMED OUT` | It exceeded the timeout |
| `PENDING` | Outside the selection (`-since`) and not judged. Not counted |

These status names are used inside gamut and in the summary on stdout. The machine-readable report maps them to mutation-testing-elements statuses as the next section describes.

### Report formats

`-reporter` chooses the formats (repeatable). The default is `text`; with `-o`, `json` is written too.

| Format | Destination | Contents |
| --- | --- | --- |
| `text` | stdout | A summary listing the survivors by reason |
| `json` | `-o` (otherwise `mutation.json` in `-report-dir`, `.gamut/report` by default) | mutation-testing-elements schema 2 |
| `html` | `mutation.html` in `-report-dir` | One HTML file embedding the `json` report |

There is only one machine-readable report, `json`.

- The mutation-testing-elements format has a published, versioned JSON Schema, is written by mutation testing tools for many languages, and works with the viewers and dashboards that read it. Keeping two formats would mean deciding both shapes for every piece of information added.
- Reports are output only; no gamut command reads them as input. Combining shards (`gamut merge`) reads the internal report kept in the cache, and unchanged reruns read the cache too. That keeps the report's shape free of internal needs. The internal report (`report.Report`) is decoupled from the outward shape, and `report.MTE` maps it.
- Statuses map as `KILLED`→Killed, `LIVED`→Survived (the kind in `statusReason`), `NOT COVERED`→NoCoverage, `NOT VIABLE`→CompileError, `TIMED OUT`→Timeout, `PENDING`→Pending. Mutants that are not counted are Ignored, with the reason in `statusReason` prefixed by `accepted: ` (accepted), `equivalent: ` (`EQUIVALENT`), or `ignored by comment: ` (disable comments). Accepted mutants keep their verdicts, so the judged status stays in `gamut.acceptedStatus`.
- Reaching tests go in `coveredBy`, and killing tests in `killedBy` (all of them for the kill matrix). gamut does not record test files, so `testFiles` lists the test names as identifiers in a single file with an empty name.
- Information not in the schema goes in an object named `gamut`, so that fields the schema adds in the future cannot collide. A mutant's `gamut` holds `original`, `mode`, `cached`, `critical`, `previouslyKilledBy`, and `acceptedStatus`; the top-level `gamut` holds `goModule`, `package`, `packages`, `mutationScore`, `testEfficacy`, `mutationsCoverage`, `thresholdBreak`, `elapsedTime`, `counts`, `staleAccepted`, `unrepeatableTests`, `maxDepthExcluded`, `skippedPackages`, and `killMatrix`. The gamut version goes in `framework.version`.
- The keys of `files` are module-relative paths, and each file includes its contents, read from the module directory. The end of `location` is the line and column after the last line break even when the replaced text spans several lines (`BRANCH_*`, `RETURN_ERROR_NIL`).
- The top-level `gamut.package` is the import path for one package, or the argument patterns joined by spaces for several, and `packages` lists the import paths of the judged packages.
- The schema requires integer thresholds, so `-threshold-high` and `-threshold-low` are rounded.
- The HTML does not bundle the viewer script (`mutation-testing-elements`); it loads the same version as the report schema (3.9.0), pinned, from a CDN. Bundling would mean managing version updates and the license. `encoding/json` escapes `<` and `>` in the report JSON, so `</script>` in embedded sources does not close the script element.
- Conformance to the schema is checked against the schema fixed in `internal/report/testdata` (mutation-testing-report-schema 3.9.0, Apache License 2.0, with the license in the same directory). The check uses a small validator inside the test that handles only the keywords this schema uses. It adds no external module and needs no network.

### Report for agents

`-reporter agent` writes one JSON document to stdout for coding agents to fill survivors.
Progress goes to stderr, so stdout can be read as JSON as it is.
It cannot be combined with `text` and `-annotations`, which also write to stdout (exit code 1).

- For each survivor to fix (unaccepted `LIVED` and `NOT COVERED`), it holds the identifier, the kind (`not detected`, `not infected`, `not covered`, `no longer detected`), the position, the line before and after the mutation, the source of the enclosing top-level declaration (Doc comment included), the reaching tests (up to 10, those whose names contain the declaration's name first, with the total), a hint on how to fix it, and whether it is in a critical region.
- Hints fit the kind and the mutator. For instance, a `not infected` boundary comparison gets "add the case where both sides are equal", and `not covered` gets "add the cases that call the declaration, in a table-driven test or with `t.Run`".
- As warnings, it lists tests that may call `t.Skip` and tests that start the test binary itself (`os.Args[0]`, `os.Executable`) with `exec.Command`. The former make mutants that are undetectable only when skipped look like survivors; for the latter, reach and failures inside the child process may not show in the verdicts. Both are approximations on the syntax tree, without types.
- Sources and test files are read again from the report and the module directory, so the same output is possible when an unchanged rerun returns the previous report.

Skills for users' agents live in `skills/` at the repository root.
`gamut` covers setup, running, configuration, exit codes, and CI; `mutation-test-loop` covers the steps to fill survivors one at a time (get them with `-reporter agent -since`, write tests for the kind, check again with `-mutant`, and accept with `gamut accept` if equivalent).

### Report of possibly unneeded code

`-reporter simplify` writes to stdout, by enclosing function, the survivors of mutations that remove code (`STATEMENT_REMOVE`, `CONDITIONS_CONSTANT`), with the text that disappears.
When removing code makes no test fail, either the tests are missing or the code is unneeded, so these are candidates for review: "remove it, or add a test that needs it".

- Candidates are only unaccepted `LIVED` with `not detected`: tests ran the code and removing it changed a value, yet no test failed.
- `not infected` (no input enters an error branch, for instance) and `NOT COVERED` usually mean inputs or tests are missing, so only their numbers are shown. They outnumber the `not detected` survivors many times over, and listing them buries the candidates.
- Only the first line of a multi-line statement is shown.
- Accepted mutants and mutants stopped by disable comments are excluded. It does not affect the score or the quality gate.
- `CONDITIONS_CONSTANT` is not in the default set, so choosing it with `-only` adds candidates.
- It cannot be combined with `agent`, which also writes to stdout.

### GitHub annotations and the Action

`-annotations github` writes the survivors to fix (unaccepted `LIVED` and `NOT COVERED`) to stdout as GitHub Actions workflow commands (`::warning file=,line=,col=,title=::`).
On a pull request, they appear as annotations on those lines.

- Survivors in critical regions fail the quality gate, so they are `::error`; the others are `::warning`. The title holds the kind and the mutator, and the body the mutation, a hint on how to fix it, and the command to accept it.
- Files are given relative to the repository, so the path from `git rev-parse --show-toplevel` to the module root is prepended.
- `%`, `\r`, and `\n` in the body, and also `:` and `,` in properties, are escaped by GitHub's rules.

The composite Action `action.yml` at the repository root installs gamut with `go install` and runs it with `-annotations github`.

- Its inputs are `since` (by default the base commit on pull requests, and a full run elsewhere; `none` forces a full run), `args` (default `./...`), `working-directory`, `cache`, and `version`. Go is installed by the user's own steps (`actions/setup-go` and so on).
- `.gamut/` is saved in the Actions cache. The key is the ref and the commit; the latest cache of the same ref is restored, or else that of the pull request's base branch. Cache records are looked up by module-relative paths, so they are reused even when the checkout location changes.
- Even when the quality gate fails, the verdicts are valid, so the cache is saved before exiting with gamut's exit code.
- With a shallow checkout, the base commit is fetched, and history is deepened 100 commits at a time until the merge base is found; if it is not found after 10 tries, the full history is fetched.

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Judging finished and the quality gate is met |
| 1 | An error in arguments, configuration, loading, building, and so on |
| 2 | Judging finished but the quality gate is not met |
| 4 | Tests fail without mutations, so nothing was judged |
| 5 | The forced-fail self-check found that mutations do not reach the tests, so nothing was judged |

The report and the summary are written even when the quality gate is not met.

### Quality gate

With only a threshold on the overall score, equivalent mutants make 100% unreachable, and tests get written just to raise the score.
So places such as authentication, authorization, and money calculations can be made critical regions, where every mutant must be detected or explicitly accepted, while the whole has a threshold.

- The score (the report's `gamut.mutationScore`) is the share of detected mutants (`KILLED`, `TIMED OUT`) in the detected plus the survivors (`LIVED`, `NOT COVERED`). The share of detected among reached mutants (`gamut.testEfficacy`) leaves out mutants nothing reaches, so it stays high even with much untested code. Thresholds apply to the score. With no mutants to judge, it is 100.
- Critical regions are the files matching the globs of `-critical` (the config's `critical`) and functions with `//gamut:critical` in their declaration's Doc. Globs match module-relative `/`-separated paths with `path.Match`, and an element `**` matches zero or more directories. Mutants in the report carry `critical`. `//gamut:critical` outside a function's Doc is ignored with a warning.
- An unaccepted survivor (`LIVED`, `NOT COVERED`) in a critical region fails the gate. Accepted mutants and `EQUIVALENT` are allowed. The summary lists critical survivors in the first group and adds the numbers of mutants and survivors in critical regions to the score line.
- The summary labels a score at or above `-threshold-high` (default 80) `high`, at or above `-threshold-low` (default 60) `low`, and below it `danger`. When `-threshold-break` (default 0, disabled) is positive, a score below it fails the gate.
- With `-fail-on-survived`, any unaccepted survivor fails the gate.
- In `-since` runs, `PENDING` does not count as surviving, so the gate applies only to the selected mutants.

## Config file

So that CI and local runs do not drift apart, settings are read from `gamut.json` at the module root (another file with `-config`).
[schema/gamut.schema.json](../../schema/gamut.schema.json) defines the format.

- There are keys only for settings that have an equivalent flag (`mutators`→`-only`, `exclude_files`→`-exclude-files`, `timeout.factor`→`-timeout-factor`, `timeout.min`→`-timeout-min`, `workers`, `tags`, `test_flags`→`-test-flags`). Config values go only into flags not given on the command line. Later stages receive the same values as when the flags are given, so the verdicts are the same.
- Unknown keys and invalid values (unknown mutators, regular expressions, durations) stop with exit code 1. A wrong key silently ignored would hide that a CI setting has no effect.
- Keys for arid rules, critical regions, and thresholds are added together with those features. Accepting them before the features would allow writing keys that do nothing.
- The config file itself is not part of the cache keys. Values that affect verdicts (mutators, tags, test flags, timeouts) enter the cache's environment by their actual values, as with flags. `exclude_files` changes only the set of mutants, so it is part of the unchanged-rerun key rather than the environment. Records of mutants in excluded files are discarded as mutants not enumerated.
- `exclude_files` regular expressions match module-relative `/`-separated paths. After excluding, mutants are numbered from 1 again (schemata uses the numbers as indexes into the helper file's arrays).
- The format is limited to JSON, which the standard library reads. YAML or TOML would add dependencies.

## Selecting the tests a change may break

`gamut select-tests -since <ref> [flags] <patterns>` chooses, from the previous verdicts and coverage records, the tests a diff may break, without running anything.
It goes through the same flag parsing, loading, and key computation as judging, and branches off just before running tests. The cache's environment is computed from the same settings as judging, so run it with the same flags as judging.

- Group 1: tests that killed mutants of changed declarations, by number of kills. For kill-matrix verdicts every killing test counts; otherwise the first one.
- Group 2: other tests that reached mutants of changed declarations, by number of reaches. If a declaration has no previous verdicts (a new declaration, or one without mutants), the package's whole test set is added.
- Group 3: changed tests and tests that transitively refer to changed test helpers (`Keys.ChangedTests`).
- Previous verdicts are looked up by file and declaration name (the declaration part of the identifier), not by mutant identifier, because rewriting a declaration can change identifiers. So the cache is only read; neither discarding vanished mutants' records nor writing happens.
- `-format run` prints, per package, a regular expression to pass to `go test -run`. `-format json` prints each test's group, kills, reaches, and the reason it was chosen.
- Without a cache, or when it was discarded because the environment changed, it stops with exit code 1.
- It runs no tests, so it finishes in well under a second.

## Kill matrix

Ordinary judging stops at the first failure, so the full set of tests that can detect a mutant is unknown.
`-kill-matrix` runs every infected test of each mutant and records every top-level test that detects it (`killedBy` of the report's mutants).

- Even without `-test.failfast`, a test binary reports failing tests by name (`--- FAIL:` lines of `-test.v`). So the infected tests of each mutant run in one process, starting one process per mutant. That is fewer launches, by the number of tests, than running each test in its own process.
- If the process crashes with a panic or `os.Exit`, the test with a `=== RUN` line and no result line counts as detecting, and the tests not yet started run again in a new process.
- Test processes are not reused. Other tests run in the same process after a detecting test, so with reuse, pollution from the previous mutant would mix into the whole detection. That makes it several times slower than an ordinary run.
- Verdicts are kept in the cache with the `matrix` mark and every killing test. Ordinary runs reuse them by the ordinary rules. Kill-matrix runs reuse `KILLED` only when it has the mark and the tests to run and their keys are unchanged. `LIVED` already ran every test, so it is reused by the ordinary rules.
- The report's `gamut.killMatrix` holds the following, and the summary has a section for it:
  - Tests that detect nothing: tests that reached mutants but detected none. Candidates for tests without assertions.
  - Subsumed tests: tests whose set of detected mutants is a proper subset of another test's set, with the tests that contain them.
  - Duplicate tests: groups of tests with the same set of detected mutants.
  - Mutants that tests reached but whose full set of killing tests is unknown (`TIMED OUT`, `PENDING`) are left out of the computation, and their number is shown.
- Subsumption is about the current mutants, not future faults, so it never suggests removing tests.
