---
status: done
stage: 4
depends_on: [004]
created_at: 2026-11-20
---

# Support CI shards and time budgets

## Motivation

Judging a large repository as a whole does not finish in time on one CI machine.

## Summary

- `-shard k/n` (`run.Shard`) splits mutants by the FNV-1a hash of their identifiers and reports the rest as `PENDING`. Splitting by identifier keeps other shards' assignments stable as mutants change, so per-shard caches stay effective.
- `gamut merge` combines the reports with `report.Merge` (the non-`PENDING` verdict wins; counts are recomputed) and the caches with `cache.Merge` (same version and environment only; quick records are dropped), and returns the summary and the quality gate's exit code.
- `-time-budget` counts from gamut's start; past it, no new mutant starts, the rest are `PENDING` and not cached. Runs with a budget skip the unchanged-rerun shortcut.
- `TestShard_MergeEqualsOneRun` and `TestTimeBudget_LeavesTheRestPending` assert both.
