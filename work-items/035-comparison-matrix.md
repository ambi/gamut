---
status: done
stage: 4
depends_on: []
created_at: 2026-11-21
---

# Compare features with major mutation testing tools

## Motivation

To decide whether to use gamut, users need to know whether it has the features common in other tools, and if not, whether they are planned.

## Summary

- Done before the release ([024](024-release-and-distribution.md)), so the stage is 4 with no dependencies.
- On 2026-11-21, gomutants was checked against its local v0.7.3 README and `--help`, the others against their official documentation and release pages. Each cell links to its source, with versions and source URLs below the table.
- Rules: what the documentation does not mention is "unknown", a feature missing from a document listing the flags or settings is "no", and language-specific aspects such as cgo are "—". PIT excludes the paid Arcmutate, noted in the cells; go-mutesting is the richer avito-tech fork.
- Rows for replacing return values and removing statements, numeric and string literals, and time budgets were added.
- gamut's gaps led to [039](039-literal-and-branch-mutators.md) (string literals stay rejected for the reasons in [018](018-more-mutators.md)); CI annotations point to [016](016-github-annotations-and-action.md) and Windows to [033](033-windows-support.md).
- The table now lives in `docs/evaluation/comparison.md`.
