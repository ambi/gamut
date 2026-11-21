# gamut guide: reports

This document covers the JSON and HTML reports, the report for agents, and the report of possibly unneeded code.

## JSON report

The JSON report (`-o`, `-reporter json`) uses version 2 of the [mutation-testing-elements](https://github.com/stryker-mutator/mutation-testing-elements) schema, so its HTML viewer and dashboards read it as is.
The keys of `files` are module-relative paths, and each file embeds its original source.
A mutant's `status` maps to gamut's verdicts as follows.

| `status` | Verdict | `statusReason` |
| --- | --- | --- |
| `Killed` | Detected | |
| `Survived` | Survived | `not infected` (reached but no value changed) or `not detected` (a value changed but no test failed) |
| `NoCoverage` | No test reached it | |
| `CompileError` | Not viable | |
| `Timeout` | Timed out (counted as detected) | |
| `Pending` | Not judged because it is outside the selection (`-since`, `-shard`, `-time-budget`) | |
| `Ignored` | Not counted | `accepted: <reason>` (accepted), `equivalent: <reason>` (equivalent), `ignored by comment: <reason>` (disable comment) |

`killedBy` lists the tests that detected the mutant (all of them with `-kill-matrix`), and `coveredBy` the tests that reached the mutated site.
Information particular to gamut is grouped in an object named `gamut`.
A mutant's `gamut` has `original` (the text before replacement), `mode` (`schemata` or `standalone`), `cached`, `critical` (whether it is in a critical region), `previouslyKilledBy` (for a survivor whose previous detecting test is gone), and `acceptedStatus` (the verdict of an accepted mutant).
The top-level `gamut` has `goModule`, `package`, `packages`, `mutationScore`, `counts` (the number per status), `staleAccepted` (acceptances no longer needed), `killMatrix`, and more.
`Pending` and `Ignored` are left out of `counts.total` and the score.

## HTML report

`-reporter html` writes `mutation.html`, which shows the report in the viewer, to `-report-dir`.
Without `-o`, `-reporter json` writes `mutation.json` to `-report-dir`.
The HTML loads the viewer's script from a CDN at a pinned version.

```console
gamut -reporter text -reporter html ./path/to/module/...
open .gamut/report/mutation.html
```

## Report for agents

When a coding agent fills survivors, `-reporter agent` hands it JSON with, per survivor, its class, the line before and after the mutation, the source of the enclosing declaration, the tests that reached it, and hints on how to fix it.
[skills/mutation-test-loop](../../skills/mutation-test-loop/SKILL.md) defines the steps, and [skills/gamut](../../skills/gamut/SKILL.md) setup and CI.

```console
gamut -reporter agent -since main ./path/to/pkg
```

## Report of possibly unneeded code

`-reporter simplify` lists, per function, code that tests run but whose removal fails no test (survivors of statement removal and constant conditions).
They are candidates to review: remove the code, or add a test that needs it.

```console
gamut -only STATEMENT_REMOVE,CONDITIONS_CONSTANT -reporter simplify ./path/to/pkg
```
