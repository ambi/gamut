# gamut guide: configuration and mutators

This document covers the config file, mutators, disable comments, and accepting and detecting equivalent mutants.

## Config file

`gamut.json` at the module root changes the defaults of the flags.
Command-line flags take precedence over the config file.
An unknown key stops gamut with exit code 1.
[schema/gamut.schema.json](../../schema/gamut.schema.json) defines the format.

```json
{
  "mutators": ["ARITHMETIC_BASE", "CONDITIONALS_BOUNDARY", "CONDITIONALS_NEGATION"],
  "exclude_files": ["_gen\\.go$", "^internal/legacy/"],
  "timeout": { "factor": 5, "min": "2s" },
  "workers": 4,
  "tags": ["integration"],
  "test_flags": ["-short"],
  "critical": ["internal/auth/**", "billing/fee.go"],
  "thresholds": { "high": 80, "low": 60, "break": 70 },
  "fail_on_survived": false
}
```

## Mutators

The default mutators are `ARITHMETIC_BASE`, `CONDITIONALS_BOUNDARY`, `CONDITIONALS_NEGATION`, `INCREMENT_DECREMENT`, `INVERT_NEGATIVES`, `RETURN_ZERO` (replaces the body of a function with results by a statement returning zero values, `nil` for `error`), and `STATEMENT_REMOVE` (skips a call statement that returns no value, except logging).
Names follow Gremlins and gomutants where they have the same mutator.
The following are off by default and used only when chosen with `-only`. The [design](../design/mutants.md#the-default-set) defines how the default set is chosen.
Code that tests rarely observe, such as logging, metrics, error messages, and the bodies of `String()`, is left unmutated. The [design](../design/mutants.md#arid-code) defines the rules, and you can add to them with the config's `arid` or with `-arid-call` and `-arid-package`.

| Name | Mutation |
| --- | --- |
| `INVERT_BITWISE` | `&`↔`\|`, `^`→`&`, `&^`→`&`, `<<`↔`>>` |
| `INVERT_ASSIGNMENTS` | `+=`↔`-=`, `*=`↔`/=`, `%=`→`*=` |
| `INVERT_BITWISE_ASSIGNMENTS` | `&=`↔`\|=`, `^=`→`&=`, `&^=`→`&=`, `<<=`↔`>>=` |
| `INVERT_LOOP_CTRL` | `break`↔`continue` |
| `INVERT_LOGICAL` | `&&`↔`\|\|` (keeping short-circuit evaluation) |
| `REMOVE_LOGICAL_NOT` | `!x`→`x` |
| `BOOLEAN_LITERAL` | `true`↔`false` |
| `CONDITIONS_CONSTANT` | An `if` condition to `true`/`false`, a `for` condition to `false`, an operand of `&&` to `true`, an operand of `\|\|` to `false` |
| `REMOVE_SELF_ASSIGNMENTS` | `x += y`→`x = y` (every compound assignment) |
| `INTEGER_INCREMENT`, `INTEGER_DECREMENT` | Add 1 to or subtract 1 from an integer literal (except the capacity of `make`) |
| `FLOAT_INCREMENT`, `FLOAT_DECREMENT` | Add 1 to or subtract 1 from a floating-point literal |
| `BRANCH_IF`, `BRANCH_ELSE`, `BRANCH_CASE` | Skip the body of an `if`, an `else`, or a `case` of a `switch` |
| `ERRORF_WRAP` | Turn `%w` in `fmt.Errorf` into `%v` |
| `RETURN_ERROR_NIL` | Turn a `return`'s result of type `error` into `nil` |

## Disable comments

Stop mutations that cannot be observed with a comment that gives the reason.

```go
return a * b //gamut:ignore -- the product is only a cache key

//gamut:ignore CONDITIONALS_BOUNDARY -- equal values never reach here
if a < b {

// Fast is a shortcut that returns the same result as Slow.
//
//gamut:ignore -- a fast path with the same result as Slow
func Fast(a, b int) bool {

//gamut:ignore-start ARITHMETIC_BASE -- defensive arithmetic
...
//gamut:ignore-end
```

A comment at the end of a line stops that line, a comment on a line of its own stops the next line, a comment right before a function declaration stops the whole function, and `ignore-start` and `ignore-end` stop everything between them.
Without a mutator name, the comment stops every mutator.
A comment without a reason gets a warning.
The [design](../design/mutants.md#disable-comments) defines the rules.

## Equivalent mutants

A mutant that no input can observe (an equivalent mutant) can be recorded as accepted in `gamut.accepted.json` at the module root, without changing the source.

```console
# Accept by passing the id from the report.
gamut accept -reason 'equivalent: callers read only the sign' 'pkg/file.go::ARITHMETIC_BASE::Double::1a2b3c4d5e'
# Judge, then remove acceptances that a test now detects and those whose mutant is gone.
gamut accept -prune ./path/to/module/...
```

Accepted mutants are still judged, but the report marks them `Ignored` (with `statusReason` `accepted: <reason>`), leaves them out of the counts and the score, and the summary does not list them among those to fix.
Acceptances no longer needed are listed in the summary and in the report's `gamut.staleAccepted`.
The [design](../design/mutants.md#accepting-equivalent-mutants) defines the rules.

`-detect-equivalent` compiles the mutated package for each survivor and reports `EQUIVALENT` for those whose machine code equals the original's (such as a mutant turning `x * 1` into `x / 1`).
It calls `go tool compile` directly, so the shared build cache does not grow.
The [design](../design/mutants.md#detecting-equivalent-mutants) defines the rules.
