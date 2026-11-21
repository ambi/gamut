---
status: done
stage: 2
depends_on: []
created_at: 2026-11-20
---

# Judge several packages in one run

## Motivation

gamut accepted only one package directory. Checking a whole module meant starting gamut per package and repeating `go list` and the preparation of export data.

## Summary

- `load.Load` takes patterns (package directories or `<directory>/...`) and returns `[]*load.Package` in import path order, running `go list -deps -export` once. Targets must be in the same module.
- All packages share one `token.FileSet` and the gc importer, guarded by a mutex; `-race` showed no races with parallel rewrites.
- `run.All` rewrites, builds, and lists tests per package in parallel, and puts coverage recording and mutant runs of all packages in one work queue. `run.Run` is `run.All` for one package.
- Packages without test files or viable mutants are not built; the former's mutants are `NOT COVERED`.
- A failing baseline or self-check in any package stops the whole run.
- The report's `package` is the import path for one package and the patterns for several; `packages` lists the judged packages.
- `testdata/multi` asserts the statuses and that they match per-package runs. On idmagic's `backend/oauth2/internal/token/...` (6 packages, 644 mutants) one run took 47.4 s against 49 s for separate runs, with identical verdicts.
