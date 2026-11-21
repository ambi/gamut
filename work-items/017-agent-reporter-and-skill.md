---
status: done
stage: 3
depends_on: [008]
created_at: 2026-11-20
---

# Provide a report and skills for agents

## Motivation

For a coding agent to fill survivors, it needs, per mutant, the class, the source of the enclosing function, the reaching tests, and the kind of fix.

## Summary

- `-reporter agent` writes one JSON document to stdout with, per survivor, the identifier, the class (four, adding `no longer detected`), the position, the line before and after, the enclosing declaration's source with its Doc, up to 10 reaching tests (those naming the declaration first) and their total, a hint, and whether it is critical. It is built from the report and the module directory, so unchanged reruns can produce it. It cannot be combined with `text` or `-annotations`, which also use stdout.
- Hints follow the class and the mutator and the way Go tests are written (table-driven, `t.Run`).
- Warnings approximate, from the syntax tree, tests that may call `t.Skip` and tests that start the test binary with `exec.Command`.
- Skills for users' agents live in `skills/gamut` and `skills/mutation-test-loop`, apart from the skills for developing gamut in `.agents/skills/`.
- Following `mutation-test-loop` on a copy of `testdata/fixture`, a table-driven test turned `Unused`'s `not covered` survivor `KILLED`, and `Limit`'s boundary survivor was accepted with a reason, since both sides return the same value at `n == max`.
