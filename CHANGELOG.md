# Changelog

This file records the changes in each version of gamut.
Versions follow [Semantic Versioning](https://semver.org/).
Until 1.0.0, a minor version may change the report's shape and how mutant identifiers are computed.

The following changes affect users' records (the record of accepted mutants, the cache, scripts that read reports), so always record them with the reason:

- A change to how mutant identifiers are computed. Acceptances in `gamut.accepted.json` and cached verdicts no longer carry over.
- A change to the report's shape (other than adding fields) or to the status names.
- A change to or removal of keys in the config file `gamut.json`.
- A change to the default set of mutators. Scores and survivor counts change.

## Unreleased

## [0.1.0] - 2026-11-21

The first version.

- Embeds every mutant in one build (mutant schemata) and never writes per-mutant build results to the shared Go build cache.
- Records coverage and infection per expression and sorts survivors into `NOT COVERED`, `LIVED` (`not infected`), and `LIVED` (`not detected`).
- 25 mutators; the default set is the 7 of `ARITHMETIC_BASE`, `CONDITIONALS_BOUNDARY`, `CONDITIONALS_NEGATION`, `INCREMENT_DECREMENT`, `INVERT_NEGATIVES`, `RETURN_ZERO`, and `STATEMENT_REMOVE`.
- Reuse of verdicts and coverage, judging only the diff (`-since`), rerunning a mutant (`-mutant`), detection by the tests of reverse dependencies (`-integration`), and judging on every save (`-watch`).
- Disable comments, accepting (`gamut accept`) and detecting (`-detect-equivalent`) equivalent mutants, critical regions, thresholds, and exit codes.
- Reports: JSON in the mutation-testing-elements format (gamut-specific data in a `gamut` object) and HTML that embeds it, JSON for agents, and GitHub annotations and an Action.
- `gamut select-tests`, `-kill-matrix`, `-reporter simplify`, `-max-depth`, `-shard` with `gamut merge`, and `-time-budget`.
- Tested on macOS and Linux. Windows is not verified.

[0.1.0]: https://github.com/ambi/gamut/releases/tag/v0.1.0
