---
status: done
stage: 3
depends_on: [004]
created_at: 2026-11-20
---

# Record equivalent mutants as accepted, outside the source

## Motivation

Equivalent mutants cannot be detected by any test, and disable comments clutter the source and scatter the reasons.

## Summary

- `internal/accept` reads and writes `gamut.accepted.json` (version and a map from identifier to reason), rejecting unknown keys and writing sorted, one field per line, via a temporary file.
- `gamut accept -reason <reason> [-dir <dir>] <id>...` records acceptances; a missing reason or a malformed identifier is an argument error.
- Accepted mutants keep their real status, get the reason, and are left out of the counts and efficacy.
- Acceptances no longer needed are listed as `detected` (a test now detects it) or `missing` (the mutant is gone). `missing` is judged only for files in the judged packages, and not under `-mutant`.
- `gamut accept -prune [flags] <patterns>...` judges first, then removes them; `-prune` must come first.
- `gamut accept`, run explicitly by the user, is the one exception to "judging never writes to the source tree", recorded in AGENTS.md and the design. The record is part of the unchanged-rerun fingerprint.
