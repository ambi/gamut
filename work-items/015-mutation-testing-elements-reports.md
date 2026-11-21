---
status: done
stage: 3
depends_on: []
created_at: 2026-11-20
---

# Write mutation-testing-elements JSON and HTML

## Motivation

The mutation-testing-elements schema (`schemaVersion: "2"`) is widely used for mutation testing reports, and writing it lets existing HTML viewers and dashboards show gamut's verdicts.

## Summary

- `-reporter` (repeatable) and `-report-dir` (default `.gamut/report`) were added.
- `json` follows schema 2 with file contents, locations, statuses, `statusReason`, `killedBy`, `coveredBy`, and `testFiles`. Uncounted mutants (disable comments, acceptances, `EQUIVALENT`) become `Ignored` with the reason; thresholds are rounded to integers.
- `html` is one file embedding the report and loading the viewer script pinned to mutation-testing-elements 3.9.0 from a CDN; the script is not bundled.
- The schema (mutation-testing-report-schema 3.9.0, Apache License 2.0) is pinned in `internal/report/testdata` and checked by a small validator in the tests, which also rejects broken reports.
- On idmagic's `usecases`, the HTML opened in headless Chrome showed the mutants per file, with a score (79.35%) matching gamut's.
