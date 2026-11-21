---
status: done
stage: 3
depends_on: []
created_at: 2026-11-20
---

# Detect equivalent mutants by comparing compiled code

## Motivation

Research on C (Trivial Compiler Equivalence, ICSE 2015) found that mutants compiling to the original's code make up about 7% of equivalent mutants. Go's compiler folds constants and removes dead branches, so the method should work well.

## Summary

- `-detect-equivalent` compiles the package per `LIVED` mutant with `go tool compile -S -dwarf=false` and marks it `EQUIVALENT` when the assembly without position notes matches the original's.
- Neither `go build -gcflags=-S` (fills the build cache) nor a temporary `GOCACHE` (rebuilds every dependency) was used; an `-importcfg` built from the loaded export data lets `go tool compile` run directly. Packages with embedded or assembly files are skipped with a warning.
- The whole package is compared rather than the enclosing function, which also catches mutations inlined into callers.
- The status name is `EQUIVALENT`, as in gomutants; such mutants are left out of the counts and efficacy. Comparisons are cached under the viability keys.
- Unit tests cover `x * 1`→`x / 1` and `x + 0`→`x - 0` (equivalent) and `x + 2`→`x - 2` (not); `testdata/equiv` covers the end-to-end flow. On idmagic's `usecases`, comparing 29 survivors took 2.2 s, did not grow the build cache, and found none equivalent.
