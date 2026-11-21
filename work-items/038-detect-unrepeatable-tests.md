---
status: done
stage: 3
depends_on: [030]
created_at: 2026-11-21
---

# Find tests that cannot be repeated and judge without reusing processes

## Motivation

Test processes are reused across mutants ([030](030-reuse-test-processes.md)), and the dirty-state check sits only on the detection side, so state left from earlier runs can make a test pass and give a false `LIVED`. In [018](018-more-mutators.md)'s fixed module, a test counting records in a package variable passed even with the call removed, so verdicts flipped between `KILLED` and `LIVED`. It also fails on the second run of plain `go test -count=2`.

## Summary

- The judging test's `cover` command runs each test again in the same process after recording, returning `u <name>` if it fails. Mutants involving such tests are judged without reusing processes.
- The mark is kept in the coverage record (`cache.TestRecord.Unrepeatable`), so runs restoring records judge the same way.
- The names appear in the report's unrepeatable tests, the summary, and the warnings of the agent report.
- `testdata/unrepeatable` toggles a package variable without resetting it; it gives both false `LIVED` and false `KILLED`, and the recheck passes too. The end-to-end test asserts verdicts equal to `-isolate` when measuring, when restoring, and without a cache; removing the condition fails all three.
- On idmagic's `usecases`, the extra time was within measurement noise and no test was unrepeatable, so restored tests are not rechecked.
