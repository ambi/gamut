---
status: done
stage: 2
depends_on: [006, 029]
created_at: 2026-11-21
---

# Reuse test processes for judging mutants

## Motivation

A new test process per mutant repeats package initialization per mutant. idmagic's `usecases` tests create two RSA keys in package variables, so each process start takes 0.1 to 0.18 s, most of the time for 463 mutants.

## Summary

- `schemata.BuildServer` adds the judging test `TestGamutServer` through the overlay. It reads "mutant-number test-name..." from stdin, switches the selection, runs the tests with `t.Run`, and writes results to file descriptor 3. It lives in the external test package (internal tests are called through exported functions); for a main package it goes in the internal test package, and processes are not reused if external tests exist.
- Each test runs as `t.Run("g", func(t) { t.Run(name, fn) })`; the outer `t.Run` waits for parallel subtests, so nothing carries over to the next mutant.
- A "cover test-name..." command records coverage without mutations, returns durations, and stops recording (`GamutCoverStop`), so the measuring process is reused for judging.
- On detection, the detecting test runs again without mutations; if it fails, the process is dirty and the mutant is judged again in a new process. Crashed and timed-out processes are thrown away. The check sits on the detection side because dirty state mostly makes tests fail.
- On `usecases` (562 mutants), the first run went from 36.5 s to 6.5 s with identical verdicts. `-isolate` remains for tests that depend strongly on process state.
