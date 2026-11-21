---
status: done
stage: 2
depends_on: [005]
created_at: 2026-11-21
---

# Record coverage in one process per test binary

## Motivation

Coverage recording started one process per test to learn each test's reach; in idmagic's integration mode (2,773 tests) the first recording alone took 115 s.

## Summary

- Test files are rewritten through the overlay to call `GamutTestStart("TestX")` at the start of each top-level test, and right after a `t.Parallel()` statement, since parallel tests resume after the others. Tests run with `-test.parallel=1`. Tests that also call `Parallel` in subtests are measured one by one. Using `=== RUN` lines as boundaries was rejected, as records go to a file and output to stdout with no ordering between them.
- The package's own tests are split into as many chunks as workers, one process per chunk; a failing chunk or a test without a boundary falls back to one-by-one measurement. Records before the first boundary (initialization) are added to every test.
- Reverse dependency tests run once per test binary; when nothing is recorded, all get empty records. Packages with reach are measured one by one; batching them was slower ([032](032-batch-revdep-coverage.md)).
- `TestAll_BatchedCoverageMatchesOneProcessPerTest` asserts identical verdicts and reaching tests. On `usecases`, recording went from 7.4 s to 0.3 s; in integration mode on `signinpolicy/domain`, from 115 s to 82 s.
