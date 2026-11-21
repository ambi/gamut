---
status: done
stage: 4
depends_on: [004]
created_at: 2026-11-20
---

# Report tests that detect nothing and subsumed tests with a kill matrix

## Motivation

A normal run stops at the first failure, so the full set of tests that can detect a mutant is unknown. Recording all of them reveals tests that detect nothing, subsumed tests, and duplicates.

## Summary

- Without `-test.failfast`, a test binary prints every failing test as `--- FAIL:`, so each mutant runs its infected tests in one process (`run.(*unit).executeAll`). After a panic, the running test without a result counts as a detector and the unstarted tests run again in a new process.
- Kill matrix runs do not reuse test processes. On idmagic's `usecases` it took 44.4 s instead of 6.9 s with the same verdicts, and found 58 subsumed tests and 4 duplicate groups.
- Cache records got `matrix` and `killers`: normal runs reuse kill matrix verdicts, and kill matrix runs rerun normal `KILLED` verdicts.
- The report got `killers` per mutant and `kill_matrix` (tests detecting nothing, subsumed tests and their subsumers, duplicate groups, counts used and excluded), with a section in the summary.
- `TestKillMatrix` (`testdata/killmatrix`) asserts each case, including tests after a panic.
