---
status: done
stage: 4
depends_on: [007]
created_at: 2026-11-20
---

# Judge the diff on every save

## Motivation

Checking detection power while editing meant starting gamut by hand after every save.

## Summary

- `-watch` (`cmd/gamut/watch.go`) repeats judging with the other arguments, adding `-since HEAD -include-test-changes` when `-since` is absent.
- The standard library has no file watching, so it compares modification times and sizes of `.go` files every 500 ms and judges 1 s after a change. The state is taken before judging and compared after, so saves during a judgment make one next judgment.
- It continues regardless of exit codes and ends on `SIGINT` or `SIGTERM`.
- `TestWatch_JudgesTheChangedFunction` asserts that the first round judges nothing and that rewriting `Mul` judges only its mutants, waiting through the test-only hook `watchHook`.
