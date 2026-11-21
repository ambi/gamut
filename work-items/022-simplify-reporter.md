---
status: done
stage: 4
depends_on: [018]
created_at: 2026-11-20
---

# Report possibly unneeded code

## Motivation

When a code-removing mutation survives, either a test is missing or the code is not needed. Listing such survivors per function and asking "remove the code, or add a test that needs it?" helps find unneeded code.

## Summary

- `-reporter simplify` (`internal/report/simplify.go`) lists unaccepted `not detected` survivors of `STATEMENT_REMOVE` and `CONDITIONS_CONSTANT` per file and enclosing function, with the removed text (the first line of multi-line statements) and the identifier.
- The plan listed `not infected` survivors with a note, but on idmagic's `usecases` 254 of 511 were `not infected` and 170 `NOT COVERED`, burying the candidates, so those two are only counted.
- It cannot be combined with `agent`, which also writes to stdout.
- `TestWriteSimplify` and `TestSimplifyReporter` (`testdata/simplify`) assert the selection and display.
