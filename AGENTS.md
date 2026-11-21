# Agent instructions

gamut is a mutation testing tool for Go.
The documents in [docs/design/](docs/design/README.md) define its design.
Before you implement or review a change, pick the topic documents you will touch from the list in `docs/design/README.md` and read them.
The user guide lives in `docs/guide/`, and the comparison with other tools lives in `docs/evaluation/`. Keep the README to an overview, installation, short usage, and the list of documents.

## Language

Write everything in the repository in English: documents, work items, code comments, CLI help, error messages, logs, and commit messages.
The one exception is [README.ja.md](README.ja.md), the Japanese edition of the README. When you change README.md, change README.ja.md in the same way.
Reports to the user in the conversation are in Japanese.
Keep identifiers, commands, mutator names (such as `ARITHMETIC_BASE`), and status names (such as `LIVED`) as they are.

## Commands

`mise.toml` holds the tool versions and the repository's commands.
Do not call `go test`, `go vet`, `gofmt`, or `golangci-lint` directly; use `mise run <task>`.

| Purpose | Command |
| --- | --- |
| Unit and end-to-end tests | `mise run test` |
| Tests of one package | `mise run test-package -- ./internal/<package>` |
| Static analysis | `mise run lint` |
| Formatting | `mise run format` |
| The full check before a change | `mise run verify` |
| Check the canary verdicts | `mise run check-canary` |
| Run gamut on any package | `mise run gamut -- <package-directory>` |

The end-to-end tests start the `go` command to build test binaries, so run them where writing to the build cache and starting processes are allowed.

## Tools

Use the following to read, write, and search repository files.
The dedicated edit tool fails when the text to replace does not match and leaves a diff, so it prevents edits that silently miss.

| Task | Use |
| --- | --- |
| Read a file | The dedicated read tool; otherwise `cat` or `sed -n 'N,Mp'` |
| Edit sources and documents | The dedicated edit tool |
| Rewrite many sites of the same shape | `ast-grep run -p <pattern> -r <rewrite> -U` (exits 1 when nothing matches) |
| Search contents | `rg` |
| Find files by name | `fd` |
| Query and extract JSON (reports, caches) | `jq` |

Use `sed -i`, and `python3` or `perl` that rewrite files, only on temporary files outside the repository (such as measurement scripts).

## Layout

| Path | Responsibility |
| --- | --- |
| `cmd/gamut` | Parsing arguments and choosing exit codes. It makes no decisions |
| `internal/config` | Reading the config file `gamut.json` and turning it into the values of the equivalent flags |
| `internal/accept` | Reading and writing the record of accepted equivalent mutants, `gamut.accepted.json` |
| `internal/equiv` | Detecting equivalent mutants by comparing a survivor's compiled code with the original |
| `internal/load` | Loading with `go list` and type-checking |
| `internal/diff` | Finding the byte ranges changed in the working tree from `git diff` |
| `internal/mutant` | Enumerating mutation sites, identifiers, and viability |
| `internal/schemata` | The schemata rewrite and the helper file for selection and coverage recording |
| `internal/run` | Building, recording and reusing coverage, selecting and running mutants, and verdicts |
| `internal/cache` | Computing the keys for reusing verdicts and coverage, and reading and writing the cache file. `internal/run` owns the reuse rules |
| `internal/report` | The JSON reports and the summary on stdout |
| `testdata/` | Fixed, self-contained Go modules for the end-to-end tests |
| `docs/design/` | The design: one document per topic, and `README.md` listing them |
| `docs/guide/` | The user guide (running, configuration and mutators, incremental runs, reports, CI) |
| `docs/evaluation/` | The feature comparison with other tools |
| `skills/` | Skills for agents of gamut's users (`gamut` for setup and CI, `mutation-test-loop` for filling survivors). Skills for developing gamut live in `.agents/skills/` |

## Invariants

- A judging run never writes to the target source tree. Rewritten files go to a temporary directory and are passed with `-overlay`. Only `gamut accept`, run explicitly by the user, writes `gamut.accepted.json` at the module root.
- The schemata binary with every mutation disabled gives the same test results as the original package. If the baseline tests fail, stop without judging.
- Mutant identifiers do not depend on lines and columns. A change to how identifiers are computed invalidates the records of users who read reports, so write the reason in [CHANGELOG.md](CHANGELOG.md). Do the same when you change the report's shape, the config file's keys, or the default set of mutators.
- Depend only on the Go standard library. To add an external module, write in the design why it beats doing it with the standard library.

## Work items

Work proceeds item by item in [work-items/](work-items/README.md).
When you start an item, set it to `status: in_progress` and check that the items in `depends_on` are done.
When you finish, confirm the completion criteria with tests, write `## Summary`, set `status: done`, and carry the decisions that stay valid into the design's topic documents.
The design records lasting decisions and a work item is a record that is done once the work is, so the design never refers to work items. Write the reasons for a decision in the design's text or its rejected ideas, not as a link to a work item. References point from work items to the design.
For new work, file an item in the format and add a row to the list before you start.

## Tests

Write behavior changes test first.
Unit-test enumeration and rewriting with tables of input source and expected output.
For changes to verdicts, assert the status of each mutant in end-to-end tests against the fixed modules in `testdata/`.
Follow the `add-mutator` skill to add a mutator.

## Releases

1. Move the "Unreleased" entries in [CHANGELOG.md](CHANGELOG.md) under a heading with the version and the date.
2. Pass `mise run verify` and commit.
3. Tag with `git tag -a vX.Y.Z -m vX.Y.Z`, and after the user confirms, publish with `git push origin main vX.Y.Z`.
4. In another environment, `go install github.com/ambi/gamut/cmd/gamut@vX.Y.Z` and check `gamut -version` and the canary check.
