---
status: done
stage: 3
depends_on: []
created_at: 2026-11-20
---

# Read a config file

## Motivation

Passing mutators, arid rules, critical regions, thresholds, and timeouts as flags every time lets CI and local runs drift apart.

## Summary

- `internal/config` reads `gamut.json` and turns its keys into the values of the equivalent flags; `cmd/gamut` puts them only into flags not given on the command line. Only JSON is supported, to stay on the standard library.
- Keys were limited to features that existed (`mutators`, `exclude_files`, `timeout`, `workers`, `tags`, `test_flags`); `arid`, `critical`, and `thresholds` arrived with their features.
- `-exclude-files` (repeatable, since regular expressions may contain commas) was added; remaining mutants are renumbered with `mutant.Without`.
- The file itself is not a cache key; values that affect verdicts enter the environment like flags.
- Unknown keys, unknown mutators, malformed values, and trailing data exit with code 1.
- `schema/gamut.schema.json` is checked against the code by unit tests, and end-to-end tests assert that keys behave like their flags and that flags win.
