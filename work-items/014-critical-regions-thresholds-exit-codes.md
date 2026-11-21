---
status: done
stage: 3
depends_on: [012]
created_at: 2026-11-20
---

# Define critical regions, thresholds, and exit codes

## Motivation

Code such as authentication or money calculations should require every mutant to be detected or explicitly accepted. A global score threshold cannot reach 100% because of equivalent mutants and invites tests written only to raise the score.

## Summary

- Critical regions are files matching `-critical` globs (`**` for any directories) and functions with `//gamut:critical` in their Doc. `mutant.Critical` returns their identifiers.
- `-threshold-high`, `-threshold-low`, and `-threshold-break` apply to the new `mutation_score`, which, unlike `test_efficacy`, counts unreached mutants. Defaults: high 80, low 60, break 0 (off).
- Failing the quality gate (unaccepted survivors in critical regions, a score below break, survivors under `-fail-on-survived`) writes the reports and summary and exits with code 2; unchanged reruns apply the gate to the replayed report.
- Exit codes became 0, 1 (errors), 2 (quality gate), 4 (baseline), 5 (self-check). Summaries of earlier items keep the codes of their time.
- `quality_test.go` reproduces every exit code.
