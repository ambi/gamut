# gamut

English | [日本語](README.ja.md)

gamut is a mutation testing tool for Go.
It makes mechanical changes (mutations) to a package's source, such as turning `<` into `<=`, and checks whether the tests detect them.

- **Fast**: it embeds every mutant in one build (mutant schemata) and picks the active one at run time. From the second run on, it judges only the mutants at changed code.
- **Leaves the build cache alone**: it never writes per-mutant build results to the shared Go build cache.
- **Sorts the survivors**: no test reached the mutant (`NOT COVERED`), tests reached it but the value did not change (`not infected`), or the value changed but no test failed (`not detected`). Each calls for a different fix.
- **No dependencies**: it uses only the Go standard library and the `go` command.

## Install

It needs Go 1.27 or later. It is tested on macOS and Linux; Windows is not verified yet.

```console
go install github.com/ambi/gamut/cmd/gamut@v0.1.0
```

Until 1.0.0, a minor version may change the report's shape and how mutant identifiers are computed. [CHANGELOG.md](CHANGELOG.md) records the changes.

## Usage

```console
# Judge the whole module and list the survivors by reason.
gamut ./...
# Judge only the mutants at code changed since main.
gamut -since main ./...
# After writing a test for a survivor, check just that mutant again.
gamut -mutant '<id from the report>' ./path/to/pkg
# Write the mutation-testing-elements JSON and the HTML that shows it.
gamut -o report.json -reporter text -reporter html ./...
```

Verdicts and coverage records are saved in `.gamut/`, and the next run redoes only what changed. Keep `.gamut/` out of the repository.

## Documents

| Document | Contents |
| --- | --- |
| [Guide: running](docs/guide/running.md) | Choosing targets, the flags, test processes, critical regions and the quality gate, exit codes |
| [Guide: configuration and mutators](docs/guide/configuration.md) | The config file `gamut.json`, mutators, disable comments, accepting and detecting equivalent mutants |
| [Guide: incremental runs](docs/guide/incremental.md) | Reusing verdicts, `-since`, `-mutant`, integration mode, `gamut select-tests` |
| [Guide: reports](docs/guide/reports.md) | JSON and HTML reports, the report for agents, the report of possibly unneeded code |
| [Guide: CI](docs/guide/ci.md) | GitHub Actions, shards and `gamut merge` |
| [Design](docs/design/README.md) | How gamut works and why |
| [Feature comparison](docs/evaluation/comparison.md) | Features compared with major mutation testing tools |

Skills for coding agents live in [skills/](skills/): `gamut` covers setup and CI, and `mutation-test-loop` the steps to fill survivors one at a time.

## Development

Run commands with `mise run <task>` (see [AGENTS.md](AGENTS.md)).

```console
mise run verify
```

## License

[MIT](LICENSE)
