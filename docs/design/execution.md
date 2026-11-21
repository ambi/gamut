# gamut design: execution

This document defines the flow of a run, starting and reusing test binaries, the schemata rewrite, the self-check that mutations reach the tests, and how cgo is handled.
The [design overview](README.md) sets out the overall purpose and roadmap.

## Flow of a run

1. **Load**: make the argument patterns (package directories or `<directory>/...`) absolute, run `go list -json -deps <patterns>...` in the directory of the first pattern to get the target packages' files, and get the locations of the dependencies' export data with `go list -export` ("Loading"). Build tags (`-tags`) go to both `go list` and `go test -c`. The target files follow `GoFiles` from `go list`; gamut never evaluates build constraints itself. The target files are parsed with `go/parser` and type-checked with `go/types`. "cgo" describes packages that use cgo.
2. **Enumerate**: enumerate mutation sites using type information. Code tests rarely observe (arid code, such as logging) is not mutated ([arid code](mutants.md#arid-code)). Generated files (`// Code generated ... DO NOT EDIT.`) are skipped.
3. **Check viability**: for each mutant, type-check the package again with `go/types` using the file with only the operator token replaced. Mutants that fail are `NOT VIABLE`. The results are cached with their keys, and only mutants whose keys changed are checked, in parallel up to the number of CPUs ([unchanged reruns](reuse.md#unchanged-reruns)).
4. **Rewrite for schemata**: write source with the viable mutants embedded to a temporary directory and type-check the rewritten package with `go/types`. Sites that cause errors leave schemata (and go to the "standalone builds" below); repeat until no errors remain.
5. **Build**: build one test binary per package with `go test -c -overlay`. The original source is never rewritten. Flags that do not change verdicts shorten the build ("Build flags"). In integration mode, the reverse dependencies' test binaries are built with the same overlay ([integration mode](selection.md#integration-mode)).
6. **Record coverage**: get the list of tests with `-test.list`, and with every mutation disabled, split the top-level tests into as many groups as there are workers, run each group in one process, and attribute records to each test at the boundary inserted at the start of each test function ([combining coverage records](reuse.md#combining-coverage-records)). For each test, record the sites it reached, the mutants whose mutated value differed from the original, and the functions it entered. Tests whose previous records can be restored are not run ([reusing coverage records](reuse.md#reusing-coverage-records)). If a test fails here, the baseline is broken, so show that test's output and stop with exit code 4. A failure of `-test.list` itself (such as a failing `TestMain` setup) is handled the same way.
7. **Self-check (forced fail)**: run the shortest test that has reach records once with `GAMUT_MUTANT=-1` and confirm that it fails. If a test whose records came from the cache can be used, run the check in parallel with coverage recording. "Self-check that mutations reach the tests" describes it.
8. **Select**: with `-since` or `-mutant`, narrow the mutants to judge and mark the rest `PENDING` ([selecting mutants to judge](selection.md#selecting-mutants-to-judge)).
9. **Reuse**: for mutants that need running, use the cached verdict without running when the previous key matches the current one. [Reusing verdicts](reuse.md#reusing-verdicts) describes it.
10. **Run**: for each mutant, run only the tests that reached it, the previous killing test first, and stop at the first failure ([execution order](selection.md#execution-order)). Test processes are reused across mutants, and a command switches the active mutant ("Reusing test processes"). With `-isolate`, a process is started per mutant, selected by `GAMUT_MUTANT`, and narrowed with `-test.run` and `-test.failfast`.
11. **Report**: write the verdicts as JSON and list the survivors by kind on stdout.

Steps 4 to 7 run per package; steps 9 and 10 put the mutants of every package in one work queue. "Several packages" describes this.
To keep infinite loops, `panic`, and polluted package variables from leaking into other mutants, a process that crashed or timed out, or that polluted its state, is thrown away.
The timeout of each test process is the sum of the coverage-recording run times of the tests it runs, times a factor, plus a minimum.

### Several packages

- Of the `go list` output, the targets are the packages that are not `DepOnly` and have non-test Go files. Packages with only test files have nothing to mutate and are dropped. Standard packages listed for the helper file are not targets either.
- The target packages must belong to one module. The keys of the report's `files` and mutant identifiers are module-relative paths, so they cannot be told apart across modules.
- All packages share one `token.FileSet` and one export-data importer. The gc importer is not safe for concurrent use, so a mutex guards it, and the `types.Package` values it reads are reused across packages.
- When target package A imports target package B, A is type-checked against B's export data before the rewrite, and A's test binary links B's unrewritten version. B's mutations are judged only by B's tests; A's tests are not tried. Integration mode handles detection by the tests of reverse dependencies.
- The schemata rewrite, the build, and listing tests run per package, in parallel up to the number of workers. Coverage recording and running mutants put the work of every package in one queue served by the same workers. The number of test processes stays at most the number of workers, however many packages there are.
- Mutant numbers (`GAMUT_MUTANT`) start at 1 in each package. Each package has its own test binary, so they never collide.
- A package without test files is not built, and all its viable mutants are `NOT COVERED`. A package without viable mutants is not built either.
- If the baseline tests or the self-check fail in any package, no package is judged. A report of only some packages is hard to tell from a full one.

### Starting test binaries

- Every run of a test binary (listing, coverage recording, the self-check, running mutants) gets the user's test flags (`-test-flags`) first and the flags gamut chooses for that run after them. testing flags without the prefix, such as `-short`, get `-test.` added; flags a test defines itself are passed as they are. gamut rejects `run`, `count`, `failfast`, `list`, and `paniconexit0`, which it chooses itself.
- On Unix, test binaries start in a new process group (`Setpgid`), and timeouts and cancellation send `SIGKILL` to the whole group, so child processes a test started neither keep using the CPU nor hold the output pipe and prolong the wait. On Windows, only the test binary is stopped. After stopping, gamut waits at most 1 second for the output pipe to close (`WaitDelay`).
- Reach and infection are recorded with an `atomic.Bool` per site and one mutex that guards appending to the file. Records are complete even when `t.Parallel` tests or many goroutines pass sites in the same process.

### Loading

Running `go list -export -deps` over the target packages compiles even the target package just edited and writes it to the build cache, though the result is never used (the test binary compiles the rewritten source).
So gamut asks for the list of dependencies (without `-export`) and, in parallel, for the export data of the packages whose export data the previous load needed (a hint recorded in the cache), and asks for anything missing afterwards.

- Export data is needed for every package other than the targets, and for target packages that another target imports.
- A stale hint (one listing a package that is gone) does not change the type-check results, since anything missing is asked for again.
- The `git diff` for `-since` and `go list -deps -test` for the keys of external dependencies also start in parallel with loading, in the module found from the argument patterns. If the loaded module differs, their results are discarded and computed again.

### Build flags

These flags are added to the test binary's build, unless the user already sets the same flag in `GOFLAGS`.

| Flag | Effect on the build | Why verdicts do not change |
| --- | --- | --- |
| `-vet=off` | Skips the vet checks | The results of the `go vet` that `go test -c` runs are not used for verdicts |
| `-gcflags=<target package>=-l` | Less compile work | Inlining does not change a program's meaning. Dependencies' flags are unchanged, so dependencies come from the build cache. Export data then holds no function bodies, so a change only to a body does not recompile the external test package either |
| `-ldflags=-s -w` | Less link work | The symbol table and debug information are separate from the table stack traces and `runtime.FuncForPC` use (pclntab) |

Disabling inlining changes escape analysis, which can break tests that assert the number of allocations with `testing.AllocsPerRun`.
That stops the run as a failing baseline, so override it with `GOFLAGS=-gcflags=` or similar.

### Reusing test processes

When package initialization (including package variables in test files) is heavy, starting a process per mutant takes most of the run.
For instance, tests whose package variables create RSA keys take 0.1-0.18 seconds per process before any test runs.
So one test process judges mutants one after another. Processes live only for one gamut run.

- The overlay adds a judging test (`TestGamutServer`, in the file `gamut_server_test.go`). It works only when the environment variable `GAMUT_SERVE` is set and skips otherwise. It lives in the external test package and calls internal tests through functions exported from the target package. For a main package it lives in the internal test package, and if the package also has external tests, processes are not reused.
- The judging test reads lines "mutant-number test-name..." from stdin, switches the selection (`GamutSetActive`), and calls the tests in order with `t.Run("g", func(t) { t.Run(name, fn) })`. The outer `t.Run` does not return until parallel subtests finish, so even `t.Parallel` tests do not carry over to the next mutant.
- Results ("run <name>", "pass", "fail <name>") are written to file descriptor 3 so they do not mix with the tests' output.
- When a mutant is detected, the detecting test runs once more without mutations. If it fails, the previous or the current mutant polluted the process's state, so the process is thrown away and the current mutant is judged again in a new process.
- When a process crashes (`panic`, `os.Exit`), the test running at the time counts as detecting, and the process is thrown away. A process that timed out is thrown away too.
- The process that records coverage in bulk is also started with the judging test and records with the command "cover test-name...". When done it stops recording (`GamutCoverStop`) and goes on to judge.
- Only the package's own test binary is reused, and only tests starting with `Test` that take one `*testing.T` are called. Mutants involving `Example`, `Fuzz`, or tests of reverse dependencies get a process per mutant.
- `-isolate` judges every mutant in its own process.

Pollution is not checked after a survivor.
It is found when the next detected mutant is checked, and that mutant is judged again in a new process.
A survivor may still come from a polluted state, but pollution tends to make tests fail, so the check sits on the detection side.

This check is not enough when a test itself leaves state behind.
A test that flips a package variable without restoring it passes, in a process left with the previous run's state, even for a mutant that removed the flip (a false `LIVED`), and fails for a mutant that kept it (a false `KILLED`; the confirming rerun flips the state again and passes).
So when coverage is recorded in the judging test's process, each test runs a second time in the same process, and a test that fails the second time is "unrepeatable".

- Mutants involving an unrepeatable test are judged in a process per mutant, not a reused one.
- The mark is kept in the cache with the coverage records (`unrepeatable`) and used in runs that restore the records.
- The test names appear in the report's `gamut.unrepeatableTests`, the summary, and the warnings of the report for agents.
- The second run only follows coverage recording, so recording takes longer by the tests' run time, without starting another process.
- Tests that pass both times but whose result depends on state, and tests whose coverage could not be recorded in the judging test's process (recorded in a separate `go test` process), are not found.

### The macOS first-launch check

macOS runs an XProtect check through syspolicyd the first time a new executable launches.
It takes a few tenths of a second even for an empty program, and the result is recorded per file.
Every code change makes a new test binary, so it costs once per run. Plain `go test` pays the same.
Launching the same binary several times at once still waits only once, so how gamut launches cannot reduce it.
Adding the terminal to developer tools in the system settings skips the check, but it disables the check for every executable launched from that terminal, so gamut does not recommend it.

### Default timeouts

The defaults are a factor of 5 and a minimum of 2 seconds.
On 2026-11-20, launching the fixture's test binary 300 times with one short test on Apple Silicon macOS took a median of 3 ms from start to exit, 8 ms at the 99th percentile, and 264 ms at most (the first launch).
With 8 in parallel the median was 5 ms and the maximum 9 ms.
The jitter for short tests is small next to the 2-second minimum, so the defaults stay.
Coverage-recording run times include starting the process and processing the records, so they come out longer than running a mutant. The limit errs on the safe side.

## Schemata rewrite

An expression with a mutation site is wrapped in a function literal that is called immediately and takes the operands as arguments.

```go
// original: if a.Count(x) < limit {
if func(l int, r int) bool {
	if gamutCovering {
		gamutHit(4)
		if (l <= r) != (l < r) { gamutInfect(11) }
		if (l >= r) != (l < r) { gamutInfect(12) }
	}
	switch gamutActive {
	case 11:
		return l <= r
	case 12:
		return l >= r
	}
	return l < r
}(a.Count(x), limit) {
```

- Operands are evaluated once, left to right, as arguments. Like the original expression, only the operation of the active mutation is computed.
- Constant operands and untyped `nil` are embedded in the body instead of passed as arguments, so type inference and constant handling stay as in the original expression.
- Argument types are the `go/types` types written with the file's import names. Sites with types that cannot be written (types of packages the file does not import, unexported types of other packages) stay out of schemata.
- For nested sites, the inner rewrite goes into the outer arguments. Each operand is written once, so deep nesting does not grow the text.
- `x++` becomes the expression statement `func() { if gamutActive == 7 { x-- } else { x++ } }()`, which also fits in the post statement of a `for`.
- The selection variable and the coverage-recording functions live in one file the overlay adds (`gamut_schemata.go`). If the target package declares the same names, the run stops.

Sites where the rewritten package fails to type-check (assignment to a named `bool` type, constant contexts, and so on) leave schemata one at a time, starting from the innermost site containing the error's position.
Mutants that leave are judged by standalone builds, which pass a file with only the operator token replaced through the overlay.
Standalone mutants have no reach records, so all tests run.

### Recording infection

Coverage-recording runs compute each mutant's mutated value and compare it with the original.
If they differ, the mutant is recorded as "infected" by that test.
Integer division, whose mutated operation can `panic`, counts as infected without comparing when the divisor is 0.
A mutant that was reached but never infected is judged a survivor (reason `not infected`) without running.
This follows the idea of weak mutation: as long as the value does not change, no test can fail.

## Self-check that mutations reach the tests

If the schemata rewrite is missing from the test binary, or `GAMUT_MUTANT` does not reach the child process, every mutant becomes `LIVED` or `NOT COVERED`, indistinguishable from weak tests.
So before judging, a run that forces mutations to fail (forced fail) confirms that mutations reach the tests.

- The helper file's `gamutHit` calls `panic("gamut: forced fail")` when `gamutActive` is `-1` (`schemata.ForcedFail`). The rewrite of each site adds no text.
- `gamutHit` is called only while recording coverage, so the check runs with both `GAMUT_COVER` and `GAMUT_MUTANT=-1`.
- It runs only the shortest test that has reach records. Once reach is recorded, a binary that does not `panic` is not reading the selection variable; there is no need to run every test.
- If the test passes or times out, gamut stops without judging, with exit code 5. If no test has reach records, it warns that the check was skipped and continues.
- Standalone mutants write the mutation directly, so there is no path for it to miss, and they are not checked.
- If the target code swallows `panic` with `recover`, the check can fail even though the selection arrives. The error message mentions this possibility too.

## cgo

In packages that use cgo (those with `CgoFiles`), only the files without `import "C"` (`GoFiles`) are mutated.

- Type-checking needs the files after cgo preprocessing. Only when there are cgo packages, gamut gets `CompiledGoFiles` with `go list -e -compiled` and uses the ones not in `GoFiles` (files cgo generated) only for type-checking, like generated files.
- Files with `import "C"` are not mutated. Preprocessed files are generated code, and mapping positions and identifiers back to the original source is costly.
- The build is left to `go test -c -overlay`, so the overlay only needs the rewrites of `GoFiles`.
- Equivalent-mutant detection would need to pass cgo's output to `go tool compile`, so it is skipped for cgo packages (`Direct` is false).
- When preprocessing fails, for instance without a C compiler, `go list -e -compiled` leaves `CompiledGoFiles` empty. That package is skipped with the reason in a warning, the report's `gamut.skippedPackages`, and the summary, and the other packages are judged. If every package is skipped, it is an error.
- If another target package imports a skipped cgo package, its export data cannot be obtained either, so type-checking fails.

## Rejected ideas

| Idea | Why not |
| --- | --- |
| Build each mutant with an overlay | Compiling and linking repeat once per mutant, and the results pile up in the shared build cache, growing it by gigabytes for a package of a few hundred mutants |
| Wrap in a selection function that computes both values (`sel(id, a+b, a-b)`) | It evaluates operands with side effects twice and causes `panic`s the original program does not have (division by zero) |
| Write the operands twice in the function literal's body | Nested sites make the text grow exponentially |
| A process per mutant (kept as `-isolate`) | With heavy package initialization, startup takes most of the run and makes it several times slower than reuse. State leaks are caught by the pollution check |
| Check for pollution after survivors too | Each mutant reruns its detecting test, nearly doubling the run on packages with many survivors. Pollution is found by the check after the next detection |
| Disable inlining in every package (`-gcflags=all=-l`) | No dependency hits the build cache anymore, so the first run is slow and the cache grows |
| Also disable optimization (`-gcflags=<target>=-N -l`) | The build is no faster than disabling inlining alone, and tests get slower |
| Recommend the developer-tools setting on macOS | It removes the first-launch check but disables the check for every executable launched from that terminal |
| Load with `golang.org/x/tools/go/packages` | It adds a dependency. `go list -export` and the export-data importer are enough |
