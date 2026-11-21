---
status: cancelled
stage: 2
depends_on: [009, 029]
created_at: 2026-11-21
---

# Measure the coverage of reverse dependency tests in batches in integration mode

## Motivation

In integration mode, reverse dependency tests that reach mutation sites are measured one process each. On idmagic's `signinpolicy/domain`, 893 such tests took 82 s of a 130 s `-no-cache` run.

## Summary

Implemented, measured slower, cancelled, and reverted.

- An import of the target was added on the package clause line of reverse dependency test files (keeping lines), boundaries were inserted, and tests were measured per chunk.
- Tests measured one by one dropped from 893 to 14, but recording grew from 82 s to 126 s and the run from 130 s to 174 s, with matching verdicts.
- Batched recording runs with `-test.parallel=1`, serializing tests with many parallel subtests, which run in parallel when measured one per process. The serialization cost more than the saved process starts.
- Rewriting reverse dependency test files also recompiles their test packages, growing the build cache on the first run.
