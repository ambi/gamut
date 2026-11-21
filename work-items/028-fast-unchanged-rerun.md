---
status: done
stage: 2
depends_on: [005]
created_at: 2026-11-21
---

# Make unchanged reruns take under a second

## Motivation

On idmagic's `usecases` (562 mutants, 135 tests), an unchanged rerun took 2.9 s: viability checks 1.47 s, building 0.52 s, listing tests 0.36 s, the rewrite 0.12 s, all giving the same results as last time.

## Summary

- Viability is recorded per identifier and key (enclosing declaration, dependencies, external dependencies; the whole package's key for constant declarations), and only mutants with changed keys are checked, in parallel up to the number of CPUs.
- When every coverage record can be restored and no mutant needs running, the package's own test binary is not built, and the test list comes from the records without a self-check.
- A run without selection returns the previous report without even running `go list` when the input fingerprint (sizes, modification times, and directory listings of the files the arguments point at, plus external dependency files) and the key of arguments and settings match (`cache.Quick`). The fingerprint does not read contents, so a content change that keeps the modification time is missed; `-no-cache` is the fallback.
- On `usecases`, the unchanged rerun went from 2.9 s to 0.05 s with matching verdicts. `TestCache_ReportsUnchangedInputsWithoutLoading` asserts the behavior.
