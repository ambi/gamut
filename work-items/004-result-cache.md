---
status: done
stage: 2
depends_on: [003]
created_at: 2026-11-20
---

# Cache verdicts under position-independent keys and skip unchanged mutants

## Motivation

gamut ran every mutant again on every run, even unchanged reruns. A per-package cache cannot follow changes in imported packages, so the dependencies must be part of each mutant's key.

## Summary

- `internal/cache` computes keys (`Keys`) and reads and writes the file (`Store`, `Environment`); the reuse rules live in `reusable` in `internal/run`. `load.SourceDeps` gets the test binary's local dependencies with `go list -deps -test`.
- The cache lives at `-cache` (default `.gamut/cache.json` in the working directory), not at the module root, so judging never writes to the target source tree.
- Coverage is recorded every run, so `NOT COVERED` and `not infected` need no cache. `KILLED` is reused only when the detecting test still runs, and `LIVED` only when the sets of reaching and run tests are unchanged.
- Test files are not type-checked (export data of test-only imports is missing); references are approximated by name. Test keys include only test file declarations.
- Dependencies include edges from interface method calls to methods of the same name and from every declaration to `init`; external dependencies include embedded files and `testdata`. The environment key includes the gamut executable's contents.
- Runs finished after cancellation are not recorded, since stopped tests look like failures; `SIGTERM` counts as cancellation.
- `cmd/gamut/cache_test.go` asserts the executed mutants and the match with uncached verdicts across edits. On three idmagic packages (593 mutants): 40.0 s uncached, 10.4 s unchanged (0 run), 13.1 s after a one-line helper change (33 run).
