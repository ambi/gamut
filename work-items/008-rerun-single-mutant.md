---
status: done
stage: 2
depends_on: [004]
created_at: 2026-11-20
---

# Rerun mutants selected by identifier

## Motivation

After writing a test for a survivor, the user wants to check just that mutant, but gamut could only run every mutant again.

## Summary

- `-mutant` takes an identifier or a unique prefix, preferring exact matches. No match or several matches list the candidates and exit with code 1.
- Only packages containing the selected mutants are prepared. The schemata still embed every mutant, so coverage numbers and cache records are shared with normal runs.
- Selected mutants ignore the cache and write their verdicts back. Tests that reached their sites are remeasured.
- `TestMutant_RerunsOnlyTheNamedMutant` asserts that a survivor turns `KILLED` after adding a test, that the report holds only that mutant, that one test is measured, and that the verdict is reused next time.
