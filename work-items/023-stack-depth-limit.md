---
status: done
stage: 4
depends_on: []
created_at: 2026-11-20
---

# Limit coverage by call depth

## Motivation

Infrastructure functions that many tests pass through by chance get hundreds of reaching tests, slowing mutant runs and blurring which test guards what.

## Summary

- With `-max-depth`, during coverage recording only, the helper's `gamutHit` and `gamutInfect` walk the frames up to the nearest `testing.tRunner` with `runtime.Callers`, counting module functions. Reach beyond the limit is recorded as "d" and counts as neither reach nor infection.
- Consecutive frames of the same declaration count as one, since the schemata wrap sites in function literals.
- The helper imports `runtime` and `strings` under aliases to avoid colliding with the target package.
- The limit is part of the cache environment; sites reached only beyond it are kept as `deep`, and the removed tests and reaches are reported.
- `TestMaxDepth` (`testdata/depth`) asserts limits 2, 3, and none, both when measuring and when restoring records. On idmagic's `usecases`, limit 4 removed 123 reaches of 9 tests, cutting 6.9 s to 5.7 s, and raised `NOT COVERED` from 113 to 136.
