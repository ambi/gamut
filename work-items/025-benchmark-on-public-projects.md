---
status: pending
stage: release
depends_on: [004, 007]
created_at: 2026-11-20
---

# Compare with gomutants and Gremlins on public projects

## Motivation

Judging the goal needs reproducible measurements on public projects. The measurements so far cover one idmagic package and only first runs.

## Scope

- Pick 3 to 5 public Go projects, mixing small fast libraries without external resources and projects with many packages (such as CLIs).
- Run gamut, gomutants, and Gremlins with the same mutators and workers, and measure the first run, an unchanged rerun, a rerun after changing one function (gamut with `-since`, the others with their incremental features), the growth of the shared build cache, and the agreement of verdicts on mutants all generate, explaining every disagreement.
- Put scripts automating the measurements in `bench/` and the results in a document.

## Design

Average two runs per measurement and make sure nothing else loads the machine; leftover processes can make measurements vary two- to threefold. Check that no test binary processes remain before and after measuring gamut.

## Completion criteria

- The results document has the date, environment, versions, and a table of each measurement above.
- Every disagreement is explained.
- Rerunning the scripts gives results with the same trends.
