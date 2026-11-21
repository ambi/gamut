---
status: done
stage: 3
depends_on: []
created_at: 2026-11-20
---

# Stop mutations with disable comments

## Motivation

Shortcuts for speed and defensive branches often have mutations that cannot be observed, and such mutants need to be stopped in place with a reason.

## Summary

- `mutant.Ignores` reads `//gamut:` comments and removes stopped mutants right after enumeration, so they are neither type-checked, run, nor counted; the rest are renumbered.
- A comment applies to its own line when that line has code, otherwise the next line; in a function declaration's Doc it applies to the whole function; `ignore-start`/`ignore-end` cover ranges.
- No status was added; stopped mutants are reported apart (later as `Ignored` in mutation-testing-elements).
- Unknown mutator names exit with code 1; missing reasons, unmatched ranges, and unknown directives are warnings. Selecting a stopped mutant with `-mutant` points at the comment and exits with code 1.
- `TestIgnores_AppliesEachCommentForm` and `TestIgnore_ReportsCommentedMutantsApart` assert each form.
