---
status: done
stage: 2
depends_on: [004]
created_at: 2026-11-20
---

# Run and report only the mutants of a diff with `--since`

## Motivation

Pull request checks and coding agents want the detection power of the changed code only, but gamut ran every mutant of the package however small the change.

## Summary

- `internal/diff` finds the changed ranges of the working tree from `git diff -M --no-prefix -U0` and untracked files. Go files are matched token by token (longest common subsequence), so comment and whitespace changes make no ranges.
- Mutants are selected by intersecting their token ranges (or, with `-scope decl`, their enclosing declarations) with the diff. The rest are `PENDING` and not counted.
- Packages the diff does not touch are not built; changed test files prepare the package, as removed detecting tests may require rechecks.
- Mutants whose previous detecting test (the cache's `killer`) is gone are rechecked and reported with `previously_killed_by` when no longer detected. `-include-test-changes` rechecks survivors that changed tests reach.
- Renames carry cached verdicts over through `IDAt`.
- `cmd/gamut/select_test.go` asserts each case in a temporary Git repository.
