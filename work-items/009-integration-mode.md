---
status: done
stage: 2
depends_on: [003]
created_at: 2026-11-20
---

# Also try to detect mutants with the tests of reverse dependencies

## Motivation

Go builds a test binary per package, so behavior observed by the tests of calling packages cannot be detected by the target package's tests alone.

## Summary

- `load.ReverseDeps` finds reverse dependencies by walking `go list -deps -test` over the main module backwards. Their test binaries are built with the target's overlay in one `go test -c -o <dir>/ <packages>...`; packages with the same last element go into separate batches, the first batch warms the build cache, and the rest run in parallel.
- With several targets, reverse dependency binaries are split per target, since mutant numbers start at 1 per package and would collide in `GAMUT_MUTANT`.
- Reverse dependency tests are named `<import path>.<test name>`, with one cache key per package.
- Unreached packages are represented by coverage records: a reverse dependency whose records can all be restored is not built until needed, so unreached packages stay unbuilt while unchanged.
- `TestIntegration_DetectsWithReverseDependencyTests` asserts `LIVED`/`NOT COVERED` turning `KILLED` under `-integration`, the report of unreached packages, and the build behavior on reruns.
- On idmagic's `backend/signinpolicy/domain` (27 mutants, 110 reverse dependencies), gamut took 163 s against 748 s for gomutants 0.7.3 `--integration`, with matching verdicts on the shared 27 mutants. 101 of the 110 packages reached no mutation site.
