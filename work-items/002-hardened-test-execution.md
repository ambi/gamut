---
status: done
stage: 2
depends_on: []
created_at: 2026-11-20
---

# Harden test execution

## Motivation

Real projects have tests behind build tags, test flags such as `-short`, tests that start child processes, and packages with `TestMain`. gamut could pass neither tags nor test flags, and could leave child processes behind on a timeout.

## Summary

- `-tags` goes through `load.Load` into `Package.Tags`, and from there to `go list` and `go test -c` (standalone builds included). Type-checking follows `go list`'s `GoFiles`.
- `-test-flags` is parsed by `run.ParseTestFlags`, which adds `-test.` to unprefixed testing flags and rejects `run`, `count`, `failfast`, `list`, and `paniconexit0`, which gamut sets per run. Every test binary start goes through `testCommand`.
- On Unix, test binaries start in a new process group, and timeouts and cancellation send `SIGKILL` to the whole group. `testdata/orphan` shows that without the group a child process stays behind.
- Failing baseline tests and a failing `-test.list` are `run.ErrBaseline`, exit code 4, with the output shown.
- The timeout defaults (factor 5, minimum 2 s) stay, based on measured start-up times (median 3 ms, maximum 264 ms).
- Concurrent recording was already safe with `atomic.Bool` and a mutex; `testdata/concurrent` shows every mutant `KILLED` under `t.Parallel` and many goroutines.
