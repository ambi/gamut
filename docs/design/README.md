# gamut design

gamut is a mutation testing tool: it makes mechanical changes to a Go package and checks whether the package's tests detect them.
This document sets out the purpose, how the design documents are organized, and the goals and roadmap.

## Purpose

- **Fast**: never recompile the package per mutant. Embed every mutant in one build and pick the active one at run time (mutant schemata).
- **Leave the build cache alone**: never write per-mutant build results to the shared Go build cache.
- **Make survivors easy to sort**: split surviving mutants into those no test reached, those reached without changing a value, and those that changed a value no assertion looks at.
- **Simple**: depend only on the Go standard library and the `go` command.

## Documents

The design is split by topic. Before an implementation or review, read the documents of the topics you touch.

| Document | What it defines |
| --- | --- |
| [Execution](execution.md) | The flow of a run, loading packages, build flags, starting and reusing test binaries, timeouts, the schemata rewrite, the self-check that mutations reach the tests, cgo |
| [Mutants](mutants.md) | Mutators and the default set, mutant identifiers, arid code, disable comments, accepting and detecting equivalent mutants |
| [Reuse](reuse.md) | Keys and rules for reusing verdicts, reusing coverage records, unchanged reruns |
| [Selection and order](selection.md) | Selecting by diff (`-since`) and by identifier (`-mutant`), the order of tests and mutants, integration mode, judging on every save, shards and time budgets, limiting the call depth |
| [Verdicts and reports](reporting.md) | Statuses, report formats, the report for agents, GitHub annotations and the Action, exit codes, the quality gate, the config file, selecting the tests a change may break, the kill matrix |

Each document ends with the ideas rejected for its topic and why.

## Goals and roadmap

The goal is mutation testing of Go packages fast enough to run again and again in daily development and CI, with survivors sorted so they are easy to fix.
The table lists the features and their state.

| Feature | State |
| --- | --- |
| Embed every mutant in one build | Done |
| Select tests per mutant (coverage per expression, telling apart the right side of short-circuit operators) | Done |
| Infection checks (weak mutation) | Done |
| Three kinds of survivors | Done |
| Up-front viability checks with `go/types` | Done |
| Leave logging and similar code unmutated (arid) | Done |
| Identifiers independent of position | Done |
| Self-check that mutations reach the tests (forced fail) | Done |
| Mutating constant contexts | Done (standalone builds) |
| Judge several packages in one run | Done |
| Reuse verdicts | Done |
| Reuse coverage records | Done |
| Judge only the diff (`-since`) | Done |
| Rerun a mutant (`-mutant`) | Done |
| Run the previous killing test first | Done |
| Detect with the tests of reverse dependencies (`-integration`) | Done |
| Accept equivalent mutants (recorded outside the source) | Done |
| Detect equivalent mutants (by comparing compiled code) | Done |
| Config file (`gamut.json`) | Done |
| Disable comments | Done |
| Critical regions (every mutant must be detected) | Done |
| Thresholds and exit codes | Done |
| mutation-testing-elements JSON and HTML | Done |
| GitHub annotations and the Action | Partly (still to verify on a pull request of a public repository) |
| Report for agents and skills | Done |
| More mutators (return values, removing call statements, logical operators, constant conditions) | Done |
| More mutators (numeric literals, branch bodies, error wrapping) | Done |
| Select the tests a change may break (`select-tests`) | Done |
| Kill matrix (tests that detect nothing, subsumed tests) | Done (`-kill-matrix`) |
| Report possibly unneeded code (`simplify`) | Done |
| Limit coverage by call depth | Done (`-max-depth`) |
| CI shards and time budgets | Done (`-shard`, `gamut merge`, `-time-budget`) |
| Rerun on every save (`-watch`) | Done |
| Packages using cgo | Done (files without `import "C"`) |
| Verifying Windows | Before release |
| Releases (`go install`, `-version`, changelog, CI) | Done |

The [feature comparison](../evaluation/comparison.md) lists the features of major mutation testing tools using only what each tool's official documentation or `--help` confirms.
Cells that cannot be confirmed say "unknown"; they are never filled by guessing.
gamut's "partly" and "no" cells say what is not verified yet or why gamut does not do it. Work items are done once their work is, so the comparison never refers to them.
When gamut gains a feature, update gamut's column too.

Versions follow Semantic Versioning, starting at 0.1.0. Until 1.0.0, a minor version may change the report's shape and how mutant identifiers are computed.
`gamut -version` prints the module version from the build info (`debug.ReadBuildInfo`): the version itself when installed with `go install ...@vX.Y.Z`, and the pseudo-version Go derives from the commit for a local build.
The cache's environment key tells builds apart by the executable's contents rather than the version string, so a development build with the same version string never inherits stale verdicts.
Changes that affect users' records (identifiers, the report's shape, config keys, the default set of mutators) are recorded in [CHANGELOG.md](../../CHANGELOG.md).

### Stages

- **Stage 1 (done)**: schemata, viability checks with `go/types`, coverage and infection per expression, three kinds of survivors, identifiers independent of position, the JSON report.
- **Stage 2 (done)**: faster reruns. Reusing results, judging the diff, rerunning a mutant, the self-check, several packages, integration mode.
- **Stage 3**: sorting and CI. Acceptance, disable comments, detecting equivalence, critical regions, report formats, annotations, the report for agents, more mutators and arid rules.
- **Stage 4**: measuring test quality. `select-tests`, the kill matrix, `simplify`, call depth, shards, `-watch`.

### Strengths particular to Go

Directions that use Go's properties:

- **Reliable viability from types**: `go/types` checks by the same rules as the Go compiler, so viability checks miss almost nothing.
- **Static help for coverage**: Go calls are easy to resolve statically, so even without `golang.org/x/tools`, references from `go/types` narrow down the tests a change affects.
- **A test binary is a single executable**: adding the judging test with an overlay is enough to switch mutants inside one process. Throwing a process away and starting another is cheap, so isolation can be used only when a process gets dirty.
