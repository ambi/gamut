# gamut design: reuse

This document defines reusing verdicts and coverage records, and unchanged reruns.
The [design overview](README.md) sets out the overall purpose and roadmap.

## Reusing verdicts

So that unchanged reruns and reruns after changing one function do not run unchanged mutants again, each mutant's verdict is saved with its keys in a cache file.
The self-check runs every time. Coverage recording does not run tests whose previous records can be restored ([reusing coverage records](#reusing-coverage-records)).

- The file is `-cache` (by default `.gamut/cache.json` in the working directory); `-no-cache` neither reads nor writes it. The default is the working directory, not the module root, so the target source tree is never written.
- The primary key is the mutant identifier. Identifiers do not depend on lines and columns, so they are looked up as they are, without adjusting positions. If two or more mutants share an identifier (two `init` functions of the same shape, for instance), none of them is reused.
- The cache is used only for mutants that need running (schemata mutants that were infected, and standalone mutants). `NOT COVERED`, `LIVED` (`not infected`), and `NOT VIABLE` from the type-check are decided every time from loading and coverage recording, without running.
- Mutants named with `-mutant` run without using cached verdicts, and their verdicts are written back.

### Keys

Every key is the first 64 bits of a SHA-256 over a syntax tree without positions and comments, or over file contents.

| Key | Contents |
| --- | --- |
| Enclosing declaration | The syntax tree of the top-level declaration containing the site, without positions, comments, or whitespace |
| Dependencies | The syntax trees of the declarations in the same package (generated files included) that the enclosing declaration refers to transitively through `go/types` references (`Info.Uses`). A call of an interface method counts as reaching every method of that name in the package. `init` is a dependency of every declaration |
| External dependencies | Among the test binary's dependencies that `go list -deps -test` returns, the Go files and embedded files of packages in the main module (workspace included) or behind an unversioned replace, the target package's embedded files, and the files under the target package's `testdata`. One per package |
| Test | The syntax trees of a top-level test function and the test-file declarations it refers to transitively. `TestMain` and `init` belong to every test. For `Example`, the comment with the expected output is included |
| Environment | `GOVERSION`, `GOOS`, `GOARCH`, `GOFLAGS`, `GOEXPERIMENT`, `CGO_ENABLED`, and `GOWORK` from `go env`, the contents of the gamut executable, `go.mod`, `go.sum`, `go.work`, the set of mutators, build tags, test flags, and the timeout factor and minimum. One for the whole file; if it differs, every record is discarded |

- Dependencies are per declaration and external dependencies per package, erring on the coarse side.
- Test files are not type-checked; references are approximated by matching identifier names, because loading does not produce export data for packages only tests import. Local variables with the same name count as references too, so the key errs on the coarse side.
- Development versions of gamut keep the same version string, so gamut is told apart by the executable's contents.

### Reuse rules

| Status | Reused when (each assumes the enclosing declaration and the judging mode are unchanged) |
| --- | --- |
| `KILLED` | Dependencies and external dependencies are unchanged, the killing top-level test is unchanged, and this run runs that test again (is infected by it, for schemata). If the killing test is unknown, the `LIVED` rule applies |
| `LIVED` (`not detected`) | Dependencies and external dependencies are unchanged, the sets of reaching tests and of tests to run are the same as before, and none of those tests changed |
| `TIMED OUT` | Dependencies and external dependencies are unchanged |
| `NOT VIABLE` (rejected by the compiler in a standalone build) | Dependencies and external dependencies are unchanged |

Coverage recording measures again every test that may have changed ([reusing coverage records](#reusing-coverage-records)), so reach and infection can be taken as those of the current source.
The test key does not include the package declarations a test calls.
So it misses a change to a declaration that the mutant's declaration does not refer to and only tests call, where values change but only whether the mutant is detected changes.
Changes to reach and infection are caught by comparing the sets.
Calls that take no reference form, such as through `fmt.Stringer`, are not dependencies either.
When in doubt, run with `-no-cache`.

### Writing

- A verdict is recorded each time a mutant finishes, and the file is written at exit. Runs stopped by cancellation (`SIGINT`, `SIGTERM`) or a failing baseline also write the verdicts so far.
- During the run, the file is also written every `-checkpoint-interval` (10 seconds by default; 0 disables it). A `SIGKILL` from running out of memory or a CI timeout skips the write at exit, so at most one interval is lost. It is not written per mutant because it rewrites the whole file, which is heavy when done once per mutant.
- Runs that finish after cancellation are not recorded. A stopped test looks like a failure and cannot be told from `KILLED`.
- The file is written to a temporary file in the same directory and renamed, so stopping midway never corrupts the previous file.
- Of the records of the packages judged in this run, those of mutants not enumerated this time are discarded. Records of other packages stay, so runs over different packages do not erase each other's records. One file holds the records of one module (the environment key includes `go.mod`).
- Each mutant in the report says in `gamut.cached` whether it was reused.
- At the end of each run, the run's internal report is kept in the cache (the latest one), which `gamut merge` reads to combine shards.

## Reusing coverage records

Coverage recording takes most of the time of a rerun once verdicts are reused.
So each test's reach, infection, and entered functions are saved in the cache, and only tests that may have changed are measured again.

- Records are looked up by the pair of the target package and the test binary's package, and by the top-level test's name.
- Site numbers change with every rewrite, so records use keys that do not depend on lines and columns: a site is the hash of "file, enclosing declaration, syntax tree path inside the declaration" (`SiteKey`), and a mutant is the hash part of its identifier.
- The schemata rewrite inserts `if gamutCovering { gamutEnter(N) };` right after the `{` of each function declaration with a body in the target files. It is inserted on the same line, so lines do not change. It works only in coverage-recording runs and records the functions a test entered. It is not inserted into `//go:nosplit` functions (it could exceed the stack limit).
- A declaration's reach key (`ReachOf`) is the hash of the declaration's syntax tree and of the syntax trees of declarations it depends on without going through function declarations (constants, types, variables). Called functions are not included: every function a test enters is recorded, so a change to a called function is caught by that function's key, and including it would measure again tests that only entered the caller.

A test that meets all of the following has its records restored and is not run.

| Condition | Change it catches |
| --- | --- |
| The external dependencies key equals the record's | Dependency packages in the local module, embedded files, `testdata` |
| The initialization key (package variables, `init`, declarations in generated files and their dependencies) equals the record's | Sites reached during initialization appear in every test. Generated files' functions get no entry records |
| The rewrite level of every site (`Wrapped`, `Marked`, `Untouched`) equals the record's | A different level changes the kinds of reach and infection recorded |
| The test key equals the record's | The test's body and the test-file declarations it refers to |
| The reach keys of the declarations enclosing the recorded entered functions and reached sites all still exist | Changes to and removals of declarations the test went through |
| The recorded sites and mutants still exist | Renamed files and the like |

- A new declaration is entered through a changed caller, so the caller's side measures it again.
- If two sites share a key (two `init` functions of the same shape, for instance), that package's records are not restored.
- When every coverage record can be restored and no mutant needs running, the package's own test binary is not even built. The list of tests comes from the records, and the self-check is skipped too: every verdict comes from records of runs that passed the self-check in the same environment (the gamut executable included). The list of tests comes from the records only when the recorded set of tests equals the set of top-level test functions in the test files and every test's key equals the record's.
- One case that cannot be restored correctly is a change that makes an unchanged caller enter a new method through a dynamic call (an interface). It is the same limit as `fmt.Stringer` in reusing verdicts; when in doubt, run with `-no-cache`.

So after changing one function, only the tests that had reached that function's sites are measured again.

### Combining coverage records

Starting one process per test makes coverage recording long for packages with many tests (thousands in integration mode).
So the tests are split into as many groups as there are workers, and each group runs in one process.

- The overlay rewrites test files to insert `GamutTestStart("TestX");` at the start of each top-level test function. It is inserted on the same line, so lines do not change. `GamutTestStart` clears the records of reach, infection, and entered functions and writes the boundary "t <name>" to the record file. External tests rewrite only the files that import the target package, and call exported functions.
- Tests run with `-test.parallel=1`. Tests that call `t.Parallel()` as a statement of the body get another insertion right after it: a parallel test stops at `Parallel` and resumes after the other tests, so the boundary is where it resumes. Tests that also call `Parallel` inside the body (in subtests) are measured one at a time, because another test can resume between subtests.
- Records before the first boundary (package initialization) are added to every test.
- If the process fails or a test has no boundary, that group falls back to measuring one test at a time. A failing baseline test is reported per test there.
- The test binaries of reverse dependencies first run all their tests once; if no reach to a site and no entered function is recorded, every test gets an empty record, because most reverse dependencies never reach the target package's sites. If there is reach, tests are measured one at a time.

## Unchanged reruns

In a run without selection, if the input fingerprint and the key of arguments and settings are the same as last time, the previous report is returned without even running `go list`.

- The fingerprint is the SHA-256 of the list and state (size, modification time, mode) of the files directly in the directories of the target packages (and of reverse dependencies in integration mode) and of the external dependencies' files, the names of their subdirectories, the state of the external dependencies' files, and the list and state of every file under `testdata` and under `<directory>/...`. `go.mod`, `go.sum`, `go.work`, gamut, and the Go version are covered by the cache's environment key. Directories whose names start with `.` and `node_modules` are left out, since `go` patterns ignore them.
- Contents are not read, so a change of contents that keeps the modification time is missed. When in doubt, run with `-no-cache`.
- The fingerprint is taken before tests run, so files changing during the run do not record the state before the change.
- Only reports of runs that finished judging (exit code 0 or 2) are recorded. The quality gate is applied again to the replayed report, so the exit code is the same. Flags that change the report (critical regions, thresholds, equivalence detection, excluded files, and so on) are part of the key of arguments and settings.

If the fingerprint differs, loading proceeds as usual, and viability checks cover only mutants whose keys changed.
The viability key is the keys of the enclosing declaration, dependencies, and external dependencies. Mutants of constant declarations use a key for the whole package, because the type-check extends to the values of constants across the package.

## Rejected ideas

| Idea | Why not |
| --- | --- |
| Take the boundaries of coverage records from `=== RUN` and `=== CONT` in `-test.v` output | Records go to a file and output to stdout, so their relative order is unknown |
| Cache verdicts per package | It cannot follow changes in imported packages and can return stale verdicts. Changing one function reruns every mutant in the package |
| Save the original source with the verdicts and adjust positions by diff | The report grows, and changes in dependencies are missed. Identifiers do not depend on position, so no adjustment is needed |
| When reusing coverage records, estimate reach statically from the declarations a test names | Local variables with the same name and references to declarations that are never called cause more remeasurement. Declarations called from other packages (such as `sort.Sort`) are missed. Records of entered functions work only in coverage-recording runs and are exact |
| Include in the test key the package declarations a test calls | Changing one function would rerun the mutants of other declarations that the tests calling it reach. Changes to reach and infection are caught by coverage recording every time |
