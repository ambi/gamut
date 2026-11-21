---
status: done
stage: 3
depends_on: [018]
created_at: 2026-11-21
---

# Add mutators for numeric literals, branch bodies, and error wrapping

## Motivation

gamut did not mutate numeric literals, the bodies of `if` and `case`, or `%w` in `fmt.Errorf`, so it missed off-by-one constants, tests that never check what a branch does, and error wrapping not checked with `errors.Is`. The [comparison](035-comparison-matrix.md) showed these as gaps no other mutator covers.

## Summary

- Ten opt-in mutators were added (`INTEGER_INCREMENT`, `INTEGER_DECREMENT`, `FLOAT_INCREMENT`, `FLOAT_DECREMENT`, `BRANCH_IF`, `BRANCH_ELSE`, `BRANCH_CASE`, `ERRORF_WRAP`, `RETURN_ERROR_NIL`, `REMOVE_SELF_ASSIGNMENTS`), matching gomutants 0.7.3's names.
- `REMOVE_SELF_ASSIGNMENTS` only adds `=` to the compound assignment rules and embeds like `INVERT_ASSIGNMENTS`.
- Numeric replacements are written in decimal, parenthesized when negative, and floats keep a decimal point or exponent; the capacity of `make` and map size hints are not mutated.
- On a real package, turning `10` in `const maxBytes = 64 << 10` into a call stopped the whole rewrite with an error outside the site, so sites in `const` declarations and array lengths now go to standalone builds before rewriting (`schemata.constantContexts`). Literal sites skip `Marked`, since `gamutMark` would give them the default type.
- `BRANCH_*` wraps the body in `if false { ... }` and skips clauses ending in `fallthrough` and wholly arid bodies. `RETURN_ERROR_NIL` keeps the expression in a branch that never runs, mutates only results of type `error`, and receives the value as `error` so typed nils behave as in the original.
- `fmt.Errorf` is arid, but `ERRORF_WRAP` and `RETURN_ERROR_NIL` change the error value, so that call's aridity does not stop them.
- Measured on idmagic's `usecases` and five packages heavy in `%w`, none joined the default set (reasons in the design's [default set](../docs/design/mutants.md#the-default-set)). The default set's verdicts matched the previous ones on 1,223 mutants of two packages.
