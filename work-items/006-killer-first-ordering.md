---
status: done
stage: 2
depends_on: [004]
created_at: 2026-11-20
---

# Run the previous detecting test first

## Motivation

gamut ran infected tests shortest first and stopped at the first failure. Running the test that detected the mutant last time first usually stops at the first test.

## Summary

- Within one test process tests run in declaration order regardless of `-test.run`, so the old "shortest first" did not change the order. The order is set by separate processes: the previous detecting test alone, then tests that detected other mutants at the same site, then the rest.
- The detecting top-level test is kept in the cache as `killer`.
- Mutant order: measured three times on idmagic, longest first (LPT, median 26.7 s) beat shortest first (27.6 s) and enumeration order (27.5 s), so LPT is the default. The design's "Execution order" keeps the measurements.
- `TestAll_RunsThePreviousKillerFirst` and `TestPhases_OrdersPreviousKillerSiteKillersThenTheRest` assert the order.
