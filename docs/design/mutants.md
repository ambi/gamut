# gamut design: mutants

This document defines the mutators, mutant identifiers, arid code, disable comments, and accepting and detecting equivalent mutants.
The [design overview](README.md) sets out the overall purpose and roadmap.

## Mutators

| Name | Target | Replacement |
| --- | --- | --- |
| `ARITHMETIC_BASE` | Binary `+`, `-`, `*`, `/`, `%` | `+`↔`-`, `*`↔`/`, `%`→`*` |
| `CONDITIONALS_BOUNDARY` | `<`, `<=`, `>`, `>=` | `<`↔`<=`, `>`↔`>=` |
| `CONDITIONALS_NEGATION` | `==`, `!=`, `<`, `<=`, `>`, `>=` | `==`↔`!=`, `<`↔`>=`, `>`↔`<=` |
| `INCREMENT_DECREMENT` | `++` and `--` statements | `++`↔`--` |
| `INVERT_NEGATIVES` | Unary `-` | `+` |
| `INVERT_BITWISE` (off by default) | Binary `&`, `\|`, `^`, `&^`, `<<`, `>>` | `&`↔`\|`, `^`→`&`, `&^`→`&`, `<<`↔`>>` |
| `INVERT_ASSIGNMENTS` (off by default) | Compound assignments `+=`, `-=`, `*=`, `/=`, `%=` | `+=`↔`-=`, `*=`↔`/=`, `%=`→`*=` |
| `INVERT_BITWISE_ASSIGNMENTS` (off by default) | Compound assignments `&=`, `\|=`, `^=`, `&^=`, `<<=`, `>>=` | `&=`↔`\|=`, `^=`→`&=`, `&^=`→`&=`, `<<=`↔`>>=` |
| `INVERT_LOOP_CTRL` (off by default) | `break` and `continue` statements (labeled ones included) | `break`↔`continue` |
| `INVERT_LOGICAL` (off by default) | `&&`, `\|\|` | `&&`↔`\|\|` |
| `REMOVE_LOGICAL_NOT` (off by default) | Unary `!` | Remove it (`!x`→`x`) |
| `BOOLEAN_LITERAL` (off by default) | Universe-scope `true`, `false` | `true`↔`false` |
| `RETURN_ZERO` | Bodies of functions with results (except `init`) | Return the zero values of the result types at the top (`nil` for `error`) |
| `STATEMENT_REMOVE` | Call statements without results (except logging) | Skip them (`f(x)`→`if false { f(x) }`) |
| `CONDITIONS_CONSTANT` (off by default) | Conditions of `if` and `for`, operands of `&&` and `\|\|` (except constant expressions) | `if`: `true` and `false`. `for`: `false`. Operands of `&&`: `true`. Operands of `\|\|`: `false` |
| `REMOVE_SELF_ASSIGNMENTS` (off by default) | Every compound assignment `op=` | `=` (`x += y`→`x = y`) |
| `INTEGER_INCREMENT`, `INTEGER_DECREMENT` (off by default) | Integer literals (except `make` capacities and `map` size hints) | Add 1, subtract 1. Written in decimal, in parentheses when negative (`0`→`(-1)`) |
| `FLOAT_INCREMENT`, `FLOAT_DECREMENT` (off by default) | Floating-point literals | Add 1, subtract 1. Always with a decimal point or an exponent (`1e2`→`101.0`) |
| `BRANCH_IF`, `BRANCH_ELSE`, `BRANCH_CASE` (off by default) | Non-empty bodies of `if`, `else` (not `else if`), and `switch` cases (except those ending in `fallthrough` and those made only of arid statements) | Skip the body (`{ f() }`→`{ if false { f() } }`) |
| `ERRORF_WRAP` (off by default) | `%w` in the format literal of `fmt.Errorf` | All become `%v` (`%%` is not a verb and stays) |
| `RETURN_ERROR_NIL` (off by default) | Non-`nil` results of `return` at positions where the enclosing function's result type is `error` | Return `nil` |

Names and replacements follow Gremlins and gomutants.
Each mutator's description and example are defined in one place in `internal/mutant` (`mutant.Infos`), and `-list-mutators` prints from there. A test checks that the list in the guide (`docs/guide/configuration.md`) names every mutator.
`-disable` removes mutators from the default set. It cannot be combined with `-only` (exit code 1).
Mutators that are off by default are enumerated only when chosen with `-only` (the config's `mutators`). Whether one joins the default set is decided after measuring the number of survivors and their breakdown on real packages.

### The default set

The default set is the 7 mutators without "off by default" in the table.
A mutator joins it when, measured on real packages, its survivors point to gaps in the tests and it does not add many mutants that overlap other mutators.

- `RETURN_ZERO` and `STATEMENT_REMOVE` are in the default set. Their survivors are functions whose results go unchecked and side effects that go unchecked (such as emitting audit events), which are gaps in the tests, and they add only a modest share of mutants.
- `CONDITIONS_CONSTANT` stays out. It more than doubles the default set's mutants, many of its `not infected` are `err != nil -> false` (no test takes the error path), which overlap other mutators' `not covered`, and many are not viable because a variable used only in the condition becomes unused.
- `INVERT_LOGICAL` stays out: about a quarter survive, and each needs judging one by one.
- `INTEGER_*` stay out. Most of their survivors are config constants (limits, timeouts) or turn the `0` of `len(x) > 0` into `1`, which shows the same gap as `CONDITIONALS_BOUNDARY`.
- `BRANCH_IF` stays out. It adds most of the default set's mutants again, and its `not covered` overlap those of the default mutators. Like `CONDITIONS_CONSTANT`, it is chosen when wanted.
- `RETURN_ERROR_NIL` stays out: most of its mutants are `not covered` (error paths no test takes), and few survive.
- `ERRORF_WRAP` stays out: about half of the mutants reached survive. When callers never check the chain with `errors.Is`, the change is unobservable, so each needs judging one by one.
- The others stay out: they have few sites or almost no survivors, and add too little information to lengthen the default run.
- Numeric-literal sites inside `const` declarations and array lengths go to standalone builds before the rewrite. The error that a call is not constant is reported outside the site (at the start of the constant expression), so it cannot be traced to a site.

`-only`, the config, disable comments, and acceptances accept the names of mutators that are off by default too.
Bitwise operators do not short-circuit, so like the arithmetic operators they are embedded in a function literal that evaluates both sides first.
A compound assignment (`x op= v`) becomes a call statement that evaluates `v` once, passes it as an argument, and switches between the original and mutated operator by the active mutant. The target expression `x` is copied into the body, so sites inside it go to standalone builds. Whether the mutation changes the value also depends on `x` (`x *= 1` versus `x /= 1`, for instance), so reaching it counts as infection, erring toward running it to find out.
`break` and `continue` act on a loop in the same function, so they cannot be wrapped in a function literal; the statement becomes two branches, `if gamutActive == N { continue } else { break }`. Sites where the branches change the loop's termination and cause `missing return` go to standalone builds by the type-check after the rewrite. A `continue` outside a loop fails the type-check and is not viable.
`&&` and `||` may skip their right side, so only the left side is passed as an argument, and the right side is written once in the body of the function literal `gamutR`, called only when needed by the original or the mutated form. Sites inside the right side are embedded in that body, so reach records follow the original short-circuit evaluation. Knowing whether the mutation changes the value needs evaluating the right side, so reaching it counts as infection.
For `true` and `false`, only identifiers that type information shows refer to the universe-scope constants are chosen; local variables with the same name are not mutated. They become a call returning the type in place (named `bool` types included); sites where a call cannot be written, such as inside a `const` declaration, go to standalone builds by the type-check after the rewrite.
A `CONDITIONS_CONSTANT` site is its own node wrapping the condition (`mutant.ConditionSite`), with a site key separate from the sites of the expression itself. Its range equals the expression's, so the rewrite does the wrapper first and rewrites the sites inside the expression within the wrapping function. The active mutant returns the constant without evaluating the condition; otherwise the condition is evaluated and returned. Making a `for` condition `true` creates an infinite loop that only waits for the timeout, so only `false` is used. Replacing with a constant can leave a variable used only in the condition unused, making the mutant not viable.
For `STATEMENT_REMOVE`, deleting the statement could leave variables or imports used only in its arguments unused and fail to compile, so the statement becomes `if false { ... }`, keeping the references. The replacement part of the identifier is a fixed string, so rewriting the arguments does not change the identifier. In schemata it becomes `if gamutActive != N { ... }`, and sites inside the arguments are rewritten within it.
`RETURN_ZERO` inserts `return *new(T1), *new(T2);` right after the body's `{`. `T` copies the text of the result types in the signature, so it can always be written in that file and works with type parameters too. `error` also becomes `nil`: a non-`nil` value such as `errors.New` would need an import, and viability could no longer be checked by text replacement alone. The site is its own node whose range is the single `{` of the body (`mutant.BodySite`), where the function's entry record is written too. With `-since`, it is selected by whether the change intersects the body.
`REMOVE_SELF_ASSIGNMENTS` adds replacements by `=` to the compound-assignment rules and is embedded like `INVERT_ASSIGNMENTS`.
Numeric literals and the format of `ERRORF_WRAP` become a call returning the type in place (as for `BOOLEAN_LITERAL`). Passing an untyped constant to `gamutMark` gives it its default type and breaks the type of the enclosing expression, so sites that cannot be wrapped go to standalone builds.
`BRANCH_*` sites are their own node whose range is the body's statement list (inside a block's `{` and `}`, or after a case's `:`) (`mutant.BranchSite`), and like `STATEMENT_REMOVE` they keep the references with `if false { ... }`. In schemata it becomes `if gamutActive != N { ... }`, and sites inside the body are rewritten within it. Entering the body counts as infection. Mutants that make the function stop terminating when emptied (`missing return`) are not viable. The replacement part of the identifier is a fixed string.
A `RETURN_ERROR_NIL` site is its own node wrapping the result expression (`mutant.ErrorSite`), with a site key separate from the sites inside the expression. The replacement is `func() error { if false { return <expr> }; return nil }()`, keeping the reference so imports used only in the expression do not become unused. In schemata, the active mutant returns `nil` without evaluating the expression; otherwise the expression is received as an `error` and returned. Because it is received as an `error`, a typed `nil` pointer becomes a non-`nil` `error`, as in the original `return`, and a non-`nil` original value counts as infection. Positions whose result type is a concrete type implementing `error` are not mutated.
A mutant is represented as "a file, the byte offset where the range starts, the original text, and the replacement text", and viability checks and standalone builds use only this replacement.
For operator mutations, the range is the single operator token and the texts equal the tokens.
Mutations that a token cannot express (removing `!`, and so on) use the same representation with an empty replacement text or similar.
Identifiers are made from the replacement text, so the identifiers of operator mutations are the same as before the representation was widened.

## Mutant identifiers

An identifier has the form `<module-relative file>::<mutator>::<enclosing function>::<hash>`.
The hash comes from the enclosing function's name, the syntax tree path inside the function (the sequence of child indexes), the mutator, and the replacement text.
It contains no lines or columns, so adding unrelated lines does not change identifiers.
Reports also carry lines and columns for people to read.

## Arid code

No mutants are made inside code tests rarely observe (arid code). Survivors there are rarely worth a test and only add sorting work.
The rules match the package and name of the callee's `types.Object`, never the text.

| Rule | What it excludes |
| --- | --- |
| Output-only calls | Calls into `log`, `log/slog`, `expvar`, `go.opentelemetry.io/otel`, and the packages under them. Methods `Debug*`, `Info*`, `Warn*`, `Error*` whose receiver type name contains `Log` or that belong to a known logger package (zap, logrus, zerolog, logr, klog). `fmt.Fprint*` writing to `os.Stderr`. Their arguments are not mutated and the statements are not removed (`STATEMENT_REMOVE`) |
| Messages | Arguments of `panic`, `errors.New`, `fmt.Errorf`. Removing a `panic` statement stays. `%w` in the format of `fmt.Errorf` (`ERRORF_WRAP`) and results returning a `fmt.Errorf` call (`RETURN_ERROR_NIL`) change the error value, not the message, so they are mutated, unless the surrounding code is arid (inside the arguments of a log call, for instance) |
| Grouped bodies | Branch bodies made only of arid statements are not emptied (`BRANCH_*`) |
| Display methods | Bodies of the methods `String`, `GoString`, and `Error` that take no arguments and return `string` |
| Time arithmetic | Arithmetic with a call to `time.Now()` or `time.Since()` as an operand |
| Groups | `if` statements (without an init statement) and blocks made only of output-only call statements. The whole, condition included, is excluded (the statement form of Google's rule `arid(n) = simple(n) ? expert(n) : children.every(arid)`) |

- The config's `arid.calls` (`-arid-call`) are globs matched against `<package>.<name>` or `<package>.<type>.<name>`, where `*` matches `/` and `.` too. `arid.packages` (`-arid-package`) makes calls into that package and the packages under it output-only calls.
- `-no-arid` uses neither the default rules nor the configured ones.
- The number of excluded mutants appears in the report's `gamut.counts.arid` and in the summary.

- The survivors the rules exclude are mostly removals of log calls and conditions of `if` statements that only log, which tests do not observe. Some excluded mutants are killed (by tests that check error messages or logs); excluding them does not change the list of survivors to fix.
- Emitting audit events is a side effect tests should check, so it is not arid.

## Disable comments

Comments stop, in place and with a reason, the mutants of sites whose mutation cannot be observed, such as shortcuts for speed or defensive branches.

| Comment | What it stops |
| --- | --- |
| `//gamut:ignore [TYPE,...] [-- reason]` (on a line with code) | That line |
| `//gamut:ignore ...` (on a line of its own) | The next line |
| `//gamut:ignore ...` (in the Doc of a function declaration) | The whole function |
| From `//gamut:ignore-start ...` to `//gamut:ignore-end` | The lines in between; to the end of the file without an `end` |

- Following the convention of Go directive comments (`//go:`, `//nolint:`), there is no space after `//`. `// gamut:ignore` is not a directive.
- Without mutators, every mutator is stopped. An unknown mutator name stops with exit code 1, because a typo would otherwise stop every mutator unnoticed.
- A comment without a reason, an unmatched `start` or `end`, and an unknown directive (another name starting with `//gamut:`) produce a warning and the run continues.
- Ranges are lines computed from the positions of comments in `go/ast`, without relying on what the syntax tree attaches them to. Only the whole-function form uses the range of the declaration its Doc attaches to.
- Stopped mutants are removed right after enumeration, never type-checked or run, and not counted. The remaining mutants are numbered from 1 again.
- In reports they have the status `Ignored`, with `statusReason` `ignored by comment: <reason>`, and are counted in `gamut.counts.ignored`. They are not judged, so the internal report keeps them apart from judged mutants.
- Naming a stopped mutant with `-mutant` shows the comment's position and exits with code 1.

## Accepting equivalent mutants

A mutant no input can observe (an equivalent mutant) cannot be detected by any added test.
To keep the reasons in one place without changing the source, acceptances are recorded by identifier and reason in `gamut.accepted.json` at the module root.

- `gamut accept -reason <reason> [-dir <directory>] <identifier>...` records them. Identifiers are the report's `id` as they are; prefixes are not accepted. The record is written in identifier order, one line per entry.
- Only `gamut accept` writes the record; judging runs never do. The invariant of not writing to the target source tree is a promise about judging runs, and this file, recorded explicitly by the user, is outside it.
- Accepted mutants are still run and keep their verdicts. In reports they have the status `Ignored`, with `statusReason` `accepted: <reason>` and the judged status in `gamut.acceptedStatus`. They count toward neither `gamut.counts.total` nor the score, are counted in `gamut.counts.accepted`, and are not listed among the survivors to fix in the summary.
- Acceptances in judged packages whose mutants tests now detect (`KILLED`, `TIMED OUT`) or that no longer exist are listed in the report's `gamut.staleAccepted` and in the summary. Acceptances in other packages are not considered gone, since those packages were not judged. Acceptances of `PENDING` mutants, and "gone" in runs that choose mutants with `-mutant`, are not judged either.
- `gamut accept -prune [judging flags] <patterns>...` judges and then removes acceptances that are no longer needed. Whether a mutant is now detected is known only by judging, so it runs an ordinary judgment (fast with a cache). `-prune` is accepted only first, to tell it from the judging flags.
- Identifiers do not depend on position, so unrelated edits keep acceptances. When the syntax tree path inside the enclosing declaration changes, the identifier changes and shows up as "gone", so accept the new identifier.
- The record of acceptances is part of the input fingerprint for unchanged reruns, since changing acceptances changes the report. It is not part of the verdict keys.

## Detecting equivalent mutants

`-detect-equivalent` compiles the package with the mutation applied for each surviving mutant (`LIVED`) and compares it with the original compilation.
If they are the same, no input can observe the mutant, so it becomes `EQUIVALENT` (`reason: same machine code`).
The Go compiler folds constants and removes dead branches, so mutants like turning `x * 1` into `x / 1` are recognized without running tests.

- Compilation calls `go tool compile -S -dwarf=false` directly, with an `-importcfg` built from the dependencies' export data obtained while loading. It never goes through `go build`, so per-mutant compilation results are not written to the shared build cache.
- The comparison covers the whole package's assembly, without the position notes on instructions (`(file.go:12)`). A mutation replaces one token, so other functions do not change. If the enclosing function is inlined into callers, differences in the callers are caught too. Files are copied to a temporary directory, and `-trimpath` removes the difference between the original's and the mutant's paths.
- A mutant that does not compile is not equivalent.
- Packages with embedded files or assembly files need `-embedcfg` or `-symabis`, so they are not compared, with a warning.
- In reports they have the status `Ignored`, with `statusReason` `equivalent: same machine code`. They count toward neither `gamut.counts.total` nor the score, and are counted in `gamut.counts.equivalent`.
- Comparison results are cached with the same keys as viability (the enclosing declaration, dependencies, external dependencies), and mutants with unchanged keys are not compiled again. The verdict record stays `LIVED`, so removing the flag brings back `LIVED`.
- It is off by default, because it compiles the package once per survivor. Most survivors, such as boundary comparisons and negations, leave a real difference in the compiled code.

## Rejected ideas

| Idea | Why not |
| --- | --- |
| Mutate string literals | They produce many mutants in logs and error messages, most of which tests do not observe, so sorting them is a heavy burden |
| Detect equivalent mutants with `go build -gcflags=<pkg>=-S` | Each survivor's compilation result piles up in the shared build cache. A temporary `GOCACHE` rebuilds every dependency and is slow. Calling `go tool compile` directly avoids both |
