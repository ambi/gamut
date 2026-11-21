---
status: done
stage: release
depends_on: [015, 026]
created_at: 2026-11-21
---

# Make mutation-testing-elements the only machine-readable report and stop reading reports as input

## Motivation

gamut had two machine-readable reports: the JSON of `-o` (gomutants' shape plus gamut's fields), with no published specification, version, or tools, and the mutation-testing-elements format of `-reporter json`, with a versioned JSON Schema and viewers. Keeping both meant deciding two shapes for every new field, and `gamut merge` read `-o` reports as input, tying the report's shape to internal needs.

## Summary

- `-o` and `-reporter json` write mutation-testing-elements format; `-reporter gomutants` is gone (an unknown format, exit code 1). With `-o` the JSON goes there, otherwise to `mutation.json` in `-report-dir`.
- Information particular to gamut goes in `gamut` objects on mutants and at the top level, through camelCase output types (`MTEGamut`, `MTEMutantGamut`) kept apart from the internal types. The internal `report.Report` is serialized only for the cache.
- Accepted, equivalent, and comment-ignored mutants are `Ignored`, told apart by the `statusReason` prefix (`accepted: `, `equivalent: `, `ignored by comment: `); accepted ones keep their judged status in `gamut.acceptedStatus`.
- `location` ends are correct for multi-line text (`BRANCH_*`, `RETURN_ERROR_NIL`).
- `gamut merge` takes shard caches as arguments and combines only the last report each run leaves in its cache (`cache.ReadLast`). Sources are read from the module directory the shards judged if present, otherwise from the working directory's module.
- Rejected: merging the shards' mutation-testing-elements reports (reports as input again), and keeping the gomutants shape as a converter (no tools read it).
- 0.1.0 was not yet published, so the changelog's 0.1.0 entry was rewritten.
