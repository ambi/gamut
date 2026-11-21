---
name: gamut
description: Install, run, configure, and wire into CI gamut, the mutation testing tool for Go. Use when asked to add gamut, run mutation testing, or put a quality gate in CI. For fixing survivors, use mutation-test-loop.
---

# Setting up and running gamut

## Install

```console
go install github.com/ambi/gamut/cmd/gamut@latest
```

gamut embeds every mutant of a package in one build and, per mutant, runs only the tests that reached it.
Verdicts and coverage records are saved in `.gamut/` in the working directory, and the next run redoes only what changed.
Keep `.gamut/` out of the repository (add it to `.gitignore`).

## Run

```console
gamut ./...                       # the whole module
gamut -since main ./...           # only mutants intersecting the diff from main
gamut -mutant '<id>' ./path/to/pkg  # one mutant, ignoring the cache
gamut -reporter agent ./path/to/pkg # JSON for agents on stdout
gamut -reporter html ./...        # .gamut/report/mutation.html
```

The statuses in the summary are `KILLED` (detected), `LIVED` (survived: `not detected` or `not infected`), `NOT COVERED` (no test reaches it), `NOT VIABLE` (fails type-checking), `TIMED OUT`, `PENDING` (outside `-since`), and `EQUIVALENT` (machine code equals the original's).
The JSON from `-o report.json` is in mutation-testing-elements format, with the statuses `Killed`, `Survived` (classified by `statusReason`), `NoCoverage`, `CompileError`, `Timeout`, `Pending`, and `Ignored` (accepted, equivalent, disable comment). Information particular to gamut is in the `gamut` object.
To fix survivors, follow the mutation-test-loop skill.

## Configure

Put settings in `gamut.json` at the module root. Command-line flags take precedence. An unknown key gives exit code 1.

```json
{
  "exclude_files": ["_gen\\.go$"],
  "critical": ["internal/auth/**"],
  "thresholds": { "high": 80, "low": 60, "break": 70 },
  "test_flags": ["-short"]
}
```

- Files matching a `critical` glob, and functions with `//gamut:critical` in the Doc of their declaration, are critical regions: an unaccepted survivor there gives exit code 2.
- `thresholds.break` is the minimum score (detected mutants as a share of detected mutants plus survivors).
- Stop mutations that cannot be observed by writing `//gamut:ignore [TYPE,...] -- <reason>` on the line. Accept a mutant that no input can tell apart with `gamut accept -reason '<reason>' '<id>'`, without changing the source.

## Exit codes

| Code | Meaning | What to do |
| --- | --- | --- |
| 0 | Judging finished and the quality gate passed | |
| 1 | An error in the arguments, the configuration, loading, or building | Fix it as the message says |
| 2 | The quality gate failed | Handle the survivors in the summary and the report with mutation-test-loop |
| 4 | Tests fail without mutations | Fix the tests, not gamut |
| 5 | Mutations do not reach the tests (the self-check failed) | Check whether the target code swallows `panic` with `recover` |

## CI (GitHub Actions)

```yaml
- uses: actions/checkout@v6
- uses: actions/setup-go@v7
  with:
    go-version-file: go.mod
- uses: ambi/gamut@v0.1.0
  with:
    version: v0.1.0
    args: ./...
```

`version` is the gamut version the Action installs. Pin it to the same version as the Action's ref.

On a pull request it judges only the diff from the base commit and annotates the survivors on their lines.
It saves `.gamut/` to the Actions cache, even when the quality gate fails.
To judge everything, pass `since: none`.
