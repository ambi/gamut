# Work items

This document defines the format of gamut's work items and lists them up to the goal.

## Goal

Make mutation testing of Go packages something that can run again and again in daily development and CI.
The goal is reached when all three of the following hold.

- gamut has every feature in the [design's roadmap table](../docs/design/README.md#goals-and-roadmap).
- The time of a first run, an unchanged rerun, and a rerun after changing one function has been measured on several public Go projects, and the results are published ([025](025-benchmark-on-public-projects.md)).
- A version installable with `go install` is published ([024](024-release-and-distribution.md)).

## Format

Name files `<three-digit number>-<kebab-case title>.md`.
Numbers follow the order of filing, and `depends_on` expresses the order of work.

```markdown
---
status: pending        # pending, in_progress, done, cancelled
stage: 2               # the stage in the design's roadmap (2, 3, 4, or release)
depends_on: []         # numbers of items to finish first
created_at: 2026-11-20
---

# One sentence stating the result

## Motivation
## Scope
## Out of scope
## Design
## Completion criteria
## Verification
```

- **Motivation** says what gamut lacks today.
- **Design** gives the approach taken and the ideas rejected. Settle questions that could change what gets built before starting.
- **Completion criteria** are observable results, in a form tests can check.
- When an item is finished, set `status: done` and add `## Summary` at the end with the decisions later readers need, in a few lines. Do not delete item files.
- Carry the design decisions that stay valid after completion into the topic documents of the [design](../docs/design/README.md).

Items finished before the repository switched to English were condensed to their motivation and summary when translated.

## List

| Number | Stage | Title | Depends on |
| --- | --- | --- | --- |
| [001](001-forced-fail-self-check.md) | 2 | Self-check that mutations reach the tests before judging | |
| [002](002-hardened-test-execution.md) | 2 | Harden test execution (build tags, test flags, process groups) | |
| [003](003-multi-package-runs.md) | 2 | Judge several packages in one run | |
| [004](004-result-cache.md) | 2 | Cache verdicts under position-independent keys and skip unchanged mutants | 003 |
| [005](005-incremental-coverage.md) | 2 | Redo coverage recording only for affected tests | 004 |
| [006](006-killer-first-ordering.md) | 2 | Run the previous detecting test first | 004 |
| [007](007-since-diff-mode.md) | 2 | Run and report only the mutants of a diff with `--since` | 004 |
| [008](008-rerun-single-mutant.md) | 2 | Rerun mutants selected by identifier | 004 |
| [009](009-integration-mode.md) | 2 | Also try to detect mutants with the tests of reverse dependencies | 003 |
| [010](010-config-file.md) | 3 | Read a config file | |
| [011](011-disable-comments.md) | 3 | Stop mutations with disable comments | |
| [012](012-accept-equivalent-mutants.md) | 3 | Record equivalent mutants as accepted, outside the source | 004 |
| [013](013-equivalence-detection.md) | 3 | Detect equivalent mutants by comparing compiled code | |
| [014](014-critical-regions-thresholds-exit-codes.md) | 3 | Define critical regions, thresholds, and exit codes | 012 |
| [015](015-mutation-testing-elements-reports.md) | 3 | Write mutation-testing-elements JSON and HTML | |
| [016](016-github-annotations-and-action.md) | 3 | Provide GitHub annotations and an Action | 007, 014 |
| [017](017-agent-reporter-and-skill.md) | 3 | Provide a report and skills for agents | 008 |
| [018](018-more-mutators.md) | 3 | Add more mutators | 010 |
| [019](019-arid-rules.md) | 3 | Widen the arid rules and make them configurable | 010 |
| [020](020-select-tests.md) | 4 | Select and print the tests a change may break | 004 |
| [021](021-kill-matrix.md) | 4 | Report tests that detect nothing and subsumed tests with a kill matrix | 004 |
| [022](022-simplify-reporter.md) | 4 | Report possibly unneeded code | 018 |
| [023](023-stack-depth-limit.md) | 4 | Limit coverage by call depth | |
| [024](024-release-and-distribution.md) | release | Publish versions installable with `go install` | 001, 002 |
| [025](025-benchmark-on-public-projects.md) | release | Compare with gomutants and Gremlins on public projects | 004, 007 |
| [026](026-sharding-and-time-budget.md) | 4 | Support CI shards and time budgets | 004 |
| [027](027-watch-mode.md) | 4 | Judge the diff on every save | 007 |
| [028](028-fast-unchanged-rerun.md) | 2 | Make unchanged reruns take under a second | 005 |
| [029](029-single-process-coverage.md) | 2 | Record coverage in one process per test binary | 005 |
| [030](030-reuse-test-processes.md) | 2 | Reuse test processes for judging mutants | 006, 029 |
| [031](031-fast-rerun-after-edit.md) | 2 | Shorten reruns after changing one function | 005, 007, 030 |
| [032](032-batch-revdep-coverage.md) | 2 | Measure the coverage of reverse dependency tests in batches in integration mode (cancelled) | 009, 029 |
| [033](033-windows-support.md) | release | Verify that verdicts on Windows match macOS and Linux | 002, 030 |
| [034](034-cgo-packages.md) | 3 | Judge packages that use cgo | |
| [035](035-comparison-matrix.md) | 4 | Compare features with major mutation testing tools | |
| [036](036-checkpoint-cache.md) | 3 | Write verdicts to the cache at an interval during the run | 004 |
| [037](037-list-and-disable-mutators.md) | 3 | List mutators and disable them individually | |
| [038](038-detect-unrepeatable-tests.md) | 3 | Find tests that cannot be repeated and judge without reusing processes | 030 |
| [039](039-literal-and-branch-mutators.md) | 3 | Add mutators for numeric literals, branch bodies, and error wrapping | 018 |
| [040](040-mte-only-report.md) | release | Make mutation-testing-elements the only machine-readable report and stop reading reports as input | 015, 026 |
