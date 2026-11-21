---
status: done
stage: 3
depends_on: [010]
created_at: 2026-11-20
---

# Widen the arid rules and make them configurable

## Motivation

In Google's study, one mutant per line and skipping unobserved (arid) code cut mutants per change from a median of 820 to 7 and raised useful reports from 80% to 89%. gamut skipped only the arguments of `log` and `log/slog`.

## Summary

- The rules in `internal/mutant/arid.go` judge by the callee's `types.Object` package and name. Output-only calls (logging, metrics, tracing, writes to `os.Stderr`, configured calls) have neither their arguments mutated nor their statements removed. For messages (`panic`, `errors.New`, `fmt.Errorf`) only the arguments are skipped.
- A "logger type" has `Log` in its type name or belongs to a known logger package; method names alone would also skip unrelated `Info` methods.
- An `if` or block consisting only of output-only calls is arid as a whole, condition included.
- Config keys `arid.calls` and `arid.packages` got the equivalent flags `-arid-call` and `-arid-package`; `-no-arid` disables everything.
- `mutant.EnumerateWith` counts arid mutants instead of creating them; the count is in the report and summary.
- Measured on four idmagic packages (see the design's "Arid code"): on `support_http` survivors went from 63 to 56, all removed ones being logging that tests do not observe.
