---
status: done
stage: 3
depends_on: [004]
created_at: 2026-11-21
---

# Write verdicts to the cache at an interval during the run

## Motivation

Verdicts were written only at exit and on cancellation, so a `SIGKILL` from running out of memory or a CI timeout lost them all and the next run started over.

## Summary

- With a cache, the cache is written every `-checkpoint-interval` (10 s by default, 0 disables) by the same write as at exit (a temporary file, then a rename). The copy is taken under the `Store`'s mutex, so writing runs alongside judging. At exit, checkpointing stops before the final write.
- `TestCheckpoint_SurvivesSIGKILL` builds gamut, judges `testdata/slow` (200 ms per test) in a child process, sends `SIGKILL` once two verdicts are recorded, and asserts that the next run reuses them and that nothing is reused with checkpointing off. The next run uses the same executable, since the environment key includes its contents.
- `TestCheckpoint_KeepsVerdicts` checks, also with `-race`, that checkpointing does not change verdicts.
