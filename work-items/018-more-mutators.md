---
status: done
stage: 3
depends_on: [010]
created_at: 2026-11-20
---

# Add more mutators

## Motivation

gamut's five mutators only replaced operators. Mutations that remove code, such as constant conditions, removed call statements, and replaced return values, often find gaps in tests, and they are the basis of [022](022-simplify-reporter.md).

## Summary

- Ten mutators were added, named after gomutants 0.7.3's `-list-mutators` where it had the same one.
- The representation of mutants widened to "a range and its replacement text" (`OriginalText`, `ReplacementText`). Identifiers come from the replacement text, so operator mutants keep their identifiers and caches; mutations containing statements or types use fixed strings.
- Embedding: the right side of `&&`/`||` is written once in a function literal called only when needed; `break`/`continue` become two branches; compound assignments evaluate the right side once; conditions and bodies are wrapped by dedicated site nodes (`ConditionSite`, `BodySite`); sites that fail type-checking after the rewrite go to standalone builds.
- Changes from the plan: `RETURN_ZERO` returns `nil` for `error` (a non-nil value would need an import), and `CONDITIONS_CONSTANT` on a `for` condition makes only `false` (`true` loops forever).
- Measured on idmagic's `usecases`, `RETURN_ZERO` and `STATEMENT_REMOVE` joined the default set (10 to 20 percent more time; all 16 new survivors were gaps in tests). The design's "The default set" keeps the breakdown. Existing machinery tests were pinned to the previous five mutators (`classic`).
- Tests accumulating package variables gave false `LIVED` in reused processes, which became [038](038-detect-unrepeatable-tests.md).
