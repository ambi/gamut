---
status: done
stage: 2
depends_on: []
created_at: 2026-11-20
---

# Self-check that mutations reach the tests before judging

## Motivation

If the schemata rewrite is missing from the test binary, or `GAMUT_MUTANT` does not reach the test process, every mutant becomes `LIVED` or `NOT COVERED`, indistinguishable from weak tests.

## Summary

- The helper's `gamutHit` panics with `gamut: forced fail` when `GAMUT_MUTANT=-1` (`schemata.ForcedFail`). It is called only while recording coverage, so the check also passes `GAMUT_COVER`.
- After coverage recording, the shortest test with reach runs once in one process. If it passes or times out, `run.ErrSelfCheck` makes `cmd/gamut` exit with a dedicated code; with no reaching test, a warning goes to `Options.Warn`.
- Code that swallows `panic` with `recover` can trip the check; the error message names that possibility.
- The end-to-end test uses the test-only hook `run.IgnoreSelection`, which pins `gamutActive` to 0.
