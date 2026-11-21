---
status: done
stage: 3
depends_on: []
created_at: 2026-11-21
---

# Judge packages that use cgo

## Motivation

Loading stopped with an error on packages with `CgoFiles`, so `./...` on a module where only some packages use cgo stopped the whole run.

## Summary

- Only when cgo packages exist, `go list -e -compiled` runs, and the files in `CompiledGoFiles` but not in `GoFiles` (cgo's outputs) are added to `Package.Others`, used only for type-checking. Mutation targets stay `GoFiles`, so files with `import "C"` are never mutated. `go test -c -overlay` handles cgo in builds.
- Equivalence detection is not done for cgo packages, since `go tool compile` is not given cgo's outputs.
- When preprocessing fails, `CompiledGoFiles` is empty, and the package is skipped with the reason in `Skipped`; gamut warns and lists it in the report and summary. If every package is skipped, it is an error (exit code 1).
- `TestCgo` (`testdata/cgo`) asserts that with a C compiler `c/pure.go` and `plain` are judged and `c/c.go` is not mutated, and that with `CC` pointing nowhere `cgo/c` is skipped and `plain` is still judged.
