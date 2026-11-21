---
status: done
stage: 2
depends_on: [005, 007, 030]
created_at: 2026-11-21
---

# Shorten reruns after changing one function

## Motivation

On idmagic's `usecases` (562 mutants), a `-since` rerun after changing one function judged one mutant but took 1.2 to 1.3 s, mostly waiting for loading, building, and process start-up.

## Summary

- `git diff` and the dependency list start alongside loading, from the module the patterns point at, and are recomputed if loading finds another module.
- Loading requests the dependency list (without `-export`) and the export data of the previous load's hint in parallel, then only what is missing, so the just-edited target is no longer compiled. Loading went from 0.25 s to 0.16 s.
- Builds add `-vet=off`, `-gcflags=<target>=-l`, and `-ldflags=-s -w` (about 20%, 20%, and a few percent), unless the user's `GOFLAGS` sets them.
- With a different one-line edit each time, `-since` takes about 1.45 s: about 0.3 s for loading, diff, keys, and viability, 0.66 s building (0.57 s even for the unmutated package), about 0.35 s for macOS's first-launch check, and 0.1 to 0.15 s for test initialization and running. Plain `go test` on the package takes 0.7 s.
- Repeating the same edit hits the build cache and reads 0.3 to 0.5 s too fast, so measurements use different edits.
- The first-launch check of macOS is out of scope: only a system setting removes it, for every executable started from that terminal.
