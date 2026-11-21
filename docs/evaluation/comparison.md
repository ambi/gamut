# Feature comparison

The features of major mutation testing tools, checked against each tool's official documentation and `--help` on 2026-11-21.
Each cell links to its source.
"Yes", "partly", and "no" are judged only from what the documentation says; what the documentation does not mention is "unknown".
A feature missing from a document that lists the flags or settings is "no".
"—" marks an aspect that cannot be compared because of the language.
gamut's "partly" and "no" cells say what is not verified yet or why gamut does not do it.

| Aspect | gamut | gomutants | Gremlins | go-mutesting | mutator | StrykerJS | PIT | cargo-mutants | mutmut |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| How mutations are embedded | [schemata (one build)][g-schemata] | [A build per mutant (overlay)][gm-how] | [Unknown][gr-workers] | [Replaces the file and runs per mutant][gms-readme] | [schemata][mu-readme] | [schemata][st-switching] | [Rewrites bytecode in memory per mutant][pit-concepts] | [A build per mutant (a copy of the tree)][cm-workspaces] | [schemata][mm-arch] |
| Test selection by coverage | [Yes (per test, per expression)][g-infection] | [Yes (per test, per line)][gm-how] | [Partly (skips uncovered mutants; tests per package)][gr-unleash] | [No (runs all the package's tests)][gms-readme] | [Yes (per test)][mu-readme] | [Yes (per test)][st-config] | [Yes (per test, per line)][pit-faq] | [No (runs the package's tests)][cm-select] | [Yes (per test)][mm-arch] |
| Classes of survivors (not reached, not infected, not detected) | [Yes (3 classes)][g-report] | [Partly (not reached and survived)][gm-mutators] | [Partly (not reached and survived)][gr-quick] | [No (detected and survived)][gms-readme] | [Yes (3 classes)][mu-coverage] | [Partly (not reached and survived)][st-states] | [Partly (not reached and survived)][pit-concepts] | [No (survived)][cm-outcomes] | [Unknown][mm-arch] |
| Up-front viability checks | [Yes (`go/types`)][g-roadmap] | [No (judged by build failures)][gm-how] | [Unknown (build failures become NOT VIABLE)][gr-quick] | [Unknown][gms-readme] | [Yes (type checks)][mu-readme] | [Yes (TypeScript type checks, optional)][st-checker] | [No (mutants the JVM cannot load are judged at run time)][pit-concepts] | [Partly (`--check` runs only `cargo check`)][cm-list] | [Yes (mypy, pyrefly, optional)][mm-docs] |
| Reusing results | [Yes (per declaration and reaching tests)][g-reuse] | [Yes (per package and tests)][gm-how] | [No][gr-unleash] | [Unknown][gms-readme] | [Yes (per enclosing function)][mu-readme] | [Yes (`--incremental`)][st-incremental] | [Yes (history file, experimental)][pit-incremental] | [Partly (`--iterate` skips previous detections)][cm-iterate] | [Yes (per function)][mm-history] |
| Running on a diff | [Yes (`-since`)][g-since] | [Yes (`--changed-since`)][gm-pr] | [Yes (`--diff`)][gr-unleash] | [Unknown][gms-readme] | [Yes (`--since`)][mu-readme] | [No][st-incremental] | [No (in the paid Arcmutate)][pit-maven] | [Yes (`--in-diff`)][cm-diff] | [No][mm-docs] |
| Rerunning a mutant | [Yes (`-mutant`)][g-mutant] | [Yes (`--run-mutant-id`)][gm-rerun] | [No][gr-unleash] | [Unknown][gms-readme] | [Yes (`--only`)][mu-readme] | [Partly (chosen by line range)][st-incremental] | [No][pit-maven] | [Partly (chosen by a name regular expression)][cm-filter] | [Partly (chosen by a name wildcard)][mm-docs] |
| Detection by other packages' tests | [Yes (reverse dependencies, chosen by coverage)][g-integration] | [Yes (reverse dependencies, chosen by coverage)][gm-cross] | [Partly (`--integration` runs every test)][gr-unleash] | [Unknown][gms-readme] | [Unknown][mu-readme] | [Unknown][st-config] | [Partly (`crossModule`, limited)][pit-maven] | [Yes (`--test-workspace`)][cm-workspaces] | [Unknown][mm-docs] |
| Number of mutators | [25 (7 by default)][g-mutators] | [28][gm-features] | [11 (5 by default)][gr-config] | [15 groups][gms-readme] | [18][mu-readme] | [16 groups][st-mutators] | [29 (11 by default)][pit-mutators] | [6 kinds][cm-mutants] | [Unknown][mm-docs] |
| Replacing return values and removing statements | [Yes][g-mutators] | [Yes][gm-mutators] | [No][gr-config] | [Yes][gms-readme] | [Yes][mu-readme] | [Yes][st-mutators] | [Yes][pit-mutators] | [Yes][cm-mutants] | [Unknown][mm-docs] |
| Mutating numeric and string literals | [Partly (numeric)][g-mutators] (strings [not adopted](../design/mutants.md#rejected-ideas)) | [Partly (numeric)][gm-mutators] | [No][gr-config] | [Partly (numeric)][gms-readme] | [Partly (numeric, plugin)][mu-readme] | [Yes][st-mutators] | [Yes][pit-mutators] | [Unknown][cm-mutants] | [Yes][mm-docs] |
| Disable comments | [Yes][g-ignore] | [Yes][gm-ignore] | [Unknown][gr-config] | [Yes][gms-readme] | [Yes][mu-readme] | [Yes][st-disable] | [No (in the paid Arcmutate)][pit-maven] | [Yes (attributes)][cm-attrs] | [Yes][mm-docs] |
| Acceptance outside the source | [Yes (per mutant)][g-accept] | [No][gm-help] | [No][gr-config] | [Yes (checksums)][gms-readme] | [Yes (per mutant)][mu-readme] | [No][st-disable] | [Partly (globs of methods and classes)][pit-maven] | [Partly (name regular expressions)][cm-filter-config] | [No][mm-docs] |
| Detecting equivalent mutants | [Yes (comparing compiled code, optional)][g-equiv] | [Yes (comparing compiled code, optional)][gm-features] | [Unknown][gr-unleash] | [Unknown][gms-releases] | [Partly (`?.` provable by types)][mu-coverage] | [No][st-equivalent] | [Partly (excludes certain mutations)][pit-mutators] | [Unknown][cm-mutants] | [No][mm-history] |
| Config file | [Yes (`gamut.json`)][g-config] | [Yes (`.gomutants.yml`)][gm-config] | [Yes (`.gremlins.yaml`)][gr-config] | [Yes (YAML)][gms-readme] | [Yes (`mutator.config.ts` and others)][mu-readme] | [Yes (`stryker.config.json` and others)][st-usage] | [Yes (Maven settings)][pit-maven] | [Yes (`.cargo/mutants.toml`)][cm-config] | [Yes (`pyproject.toml` and others)][mm-docs] |
| Thresholds and exit codes | [Yes (including critical regions)][g-quality] | [Yes][gm-exit] | [Yes][gr-unleash] | [Unknown][gms-readme] | [Yes (including critical regions)][mu-readme] | [Yes][st-thresholds] | [Yes][pit-maven] | [Partly (exit code 2 on survivors; no ratio threshold in the list)][cm-exit] | [Unknown][mm-history] |
| mutation-testing-elements JSON | [Yes][g-formats] | [Yes][gm-stryker] | [No][gr-unleash] | [Unknown (its own JSON)][gms-readme] | [Yes][mu-readme] | [Yes][st-plugins] | [No (HTML, XML, CSV)][pit-maven] | [No (its own JSON)][cm-out] | [No][mm-docs] |
| HTML report | [Yes][g-formats] | [Yes][gm-html] | [No][gr-unleash] | [Yes][gms-readme] | [Yes][mu-readme] | [Yes][st-config] | [Yes][pit-maven] | [Unknown][cm-out] | [No][mm-docs] |
| CI annotations | [Partly (GitHub; not yet verified on a pull request of a public repository)][g-github] | [Yes (GitHub)][gm-features] | [No][gr-action] | [Unknown][gms-readme] | [Yes (GitHub)][mu-readme] | [No][st-plugins] | [No (in the paid Arcmutate)][pit-maven] | [Yes (GitHub)][cm-releases] | [No][mm-docs] |
| Report for agents | [Yes (report and skills)][g-agent] | [Partly (a Claude Code plugin)][gm-plugin] | [No][gr-unleash] | [Unknown][gms-readme] | [Yes (report and skills)][mu-readme] | [No][st-plugins] | [No][pit-maven] | [Unknown][cm-out] | [No][mm-docs] |
| CI shards | [Yes (including `merge` to combine them)][g-shard] | [No][gm-compare] | [No][gr-unleash] | [Unknown][gms-readme] | [Yes][mu-readme] | [No][st-config] | [No][pit-maven] | [Yes][cm-shards] | [No][mm-docs] |
| Time budget | [Yes][g-shard] | [No][gm-help] | [No][gr-unleash] | [Unknown][gms-readme] | [Yes][mu-readme] | [No][st-config] | [No][pit-maven] | [Unknown][cm-timeouts] | [No][mm-docs] |
| Rerunning on every save | [Yes][g-watch] | [No][gm-help] | [No][gr-unleash] | [Unknown][gms-readme] | [Yes][mu-readme] | [No][st-usage] | [No][pit-maven] | [Unknown][cm-print] | [No][mm-docs] |
| OS | [macOS, Linux (Windows not verified)][g-exec] | [macOS, Linux (no automated tests on Windows)][gm-install] | [Linux, macOS, Windows][gr-install] | [Unknown][gms-readme] | [Linux and Windows checked in CI][mu-readme] | [Unknown][st-releases] | [Unknown][pit-faq] | [Unknown][cm-print] | [POSIX (Windows via WSL)][mm-docs] |
| cgo | [Partly (files without `import "C"`)][g-cgo] | [Unknown][gm-readme] | [Unknown][gr-install] | [Unknown][gms-readme] | — | — | — | — | — |

The versions compared:

- gamut: v0.1.0 (the features compared are those of the commit just before it, e1fd6fd; 10 mutators were added after it)
- gomutants: [v0.7.3][gm-readme]
- Gremlins: [v0.6.0][gr-releases] (2025-12-06)
- go-mutesting: [v2.3.1][gms-releases] of the avito-tech fork (2025-12-26), which has more features than the original zimmski/go-mutesting
- mutator (TypeScript): [v0.5.0][mu-releases] (2026-11-19)
- StrykerJS: [v10.1.1][st-releases] (2026-10-23)
- PIT: [1.30.2][pit-releases] (2026-11-05), without the paid extension Arcmutate
- cargo-mutants: [v27.1.0][cm-releases] (2026-06-02)
- mutmut: [3.8.1][mm-pypi] (2026-10-28)

The sources:

- gamut: the [design](../design/README.md) and the [guide](../../README.md#documents)
- gomutants: the [README (v0.7.3)][gm-readme] and `gomutants --help` (v0.7.3)
- Gremlins: <https://gremlins.dev/latest/>, <https://gremlins.dev/latest/quick-start/>, <https://gremlins.dev/latest/install/>, <https://gremlins.dev/latest/usage/commands/unleash/>, <https://gremlins.dev/latest/usage/commands/unleash/workers/>, <https://gremlins.dev/latest/usage/configuration/>, <https://gremlins.dev/latest/usage/ci/github-action/>, <https://github.com/go-gremlins/gremlins/releases>
- go-mutesting: <https://github.com/avito-tech/go-mutesting>, <https://github.com/avito-tech/go-mutesting/releases>
- mutator: <https://github.com/mizchi/mutator>, <https://github.com/mizchi/mutator/blob/main/docs/coverage-and-mutation.md>, <https://github.com/mizchi/mutator/releases>
- StrykerJS: <https://stryker-mutator.io/blog/announcing-stryker-4-mutation-switching/>, <https://stryker-mutator.io/docs/stryker-js/configuration/>, <https://stryker-mutator.io/docs/stryker-js/usage/>, <https://stryker-mutator.io/docs/stryker-js/incremental/>, <https://stryker-mutator.io/docs/stryker-js/typescript-checker/>, <https://stryker-mutator.io/docs/stryker-js/disable-mutants/>, <https://stryker-mutator.io/docs/stryker-js/plugins/>, <https://stryker-mutator.io/docs/mutation-testing-elements/mutant-states-and-metrics/>, <https://stryker-mutator.io/docs/mutation-testing-elements/supported-mutators/>, <https://stryker-mutator.io/docs/mutation-testing-elements/equivalent-mutants/>, <https://github.com/stryker-mutator/stryker-js/releases>
- PIT: <https://pitest.org/quickstart/basic_concepts/>, <https://pitest.org/quickstart/mutators/>, <https://pitest.org/quickstart/maven/>, <https://pitest.org/quickstart/incremental_analysis/>, <https://pitest.org/faq/>, <https://github.com/hcoles/pitest/releases>
- cargo-mutants: <https://mutants.rs/print.html>, <https://mutants.rs/mutants.html>, <https://mutants.rs/attrs.html>, <https://mutants.rs/in-diff.html>, <https://mutants.rs/iterate.html>, <https://mutants.rs/shards.html>, <https://mutants.rs/workspaces.html>, <https://mutants.rs/mutants-out.html>, <https://mutants.rs/exit-codes.html>, <https://mutants.rs/timeouts.html>, <https://github.com/sourcefrog/cargo-mutants/releases>
- mutmut: <https://mutmut.readthedocs.io/en/latest/>, <https://github.com/boxed/mutmut/blob/main/ARCHITECTURE.rst>, <https://github.com/boxed/mutmut/blob/main/HISTORY.rst>, <https://pypi.org/project/mutmut/>

[g-schemata]: ../design/execution.md#schemata-rewrite
[g-infection]: ../design/execution.md#recording-infection
[g-report]: ../design/reporting.md#verdicts-and-reports
[g-roadmap]: ../design/README.md#goals-and-roadmap
[g-reuse]: ../design/reuse.md#reusing-verdicts
[g-since]: ../design/selection.md#diff--since
[g-mutant]: ../design/selection.md#identifiers--mutant
[g-integration]: ../design/selection.md#integration-mode
[g-mutators]: ../design/mutants.md#mutators
[g-ignore]: ../design/mutants.md#disable-comments
[g-accept]: ../design/mutants.md#accepting-equivalent-mutants
[g-equiv]: ../design/mutants.md#detecting-equivalent-mutants
[g-config]: ../design/reporting.md#config-file
[g-quality]: ../design/reporting.md#quality-gate
[g-formats]: ../design/reporting.md#report-formats
[g-github]: ../design/reporting.md#github-annotations-and-the-action
[g-agent]: ../design/reporting.md#report-for-agents
[g-shard]: ../design/selection.md#shards-and-time-budgets
[g-watch]: ../design/selection.md#judging-on-every-save
[g-exec]: ../design/execution.md#starting-test-binaries
[g-cgo]: ../design/execution.md#cgo
[gm-readme]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md
[gm-how]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#how-it-works
[gm-features]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#features
[gm-mutators]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#mutators
[gm-ignore]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#inline-ignore-directives
[gm-compare]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#how-it-compares
[gm-pr]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#pr-scoped-mode
[gm-cross]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#cross-package-mode
[gm-rerun]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#re-running-a-single-mutant
[gm-config]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#configuration-file
[gm-exit]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#exit-codes--ci-integration
[gm-stryker]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#stryker-format-reports
[gm-html]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#html-reports
[gm-plugin]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#claude-code-plugin
[gm-install]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#installation
[gm-help]: https://github.com/gomutants/gomutants/blob/v0.7.3/README.md#all-flags
[gr-quick]: https://gremlins.dev/latest/quick-start/
[gr-install]: https://gremlins.dev/latest/install/
[gr-unleash]: https://gremlins.dev/latest/usage/commands/unleash/
[gr-workers]: https://gremlins.dev/latest/usage/commands/unleash/workers/
[gr-config]: https://gremlins.dev/latest/usage/configuration/
[gr-action]: https://gremlins.dev/latest/usage/ci/github-action/
[gr-releases]: https://github.com/go-gremlins/gremlins/releases
[gms-readme]: https://github.com/avito-tech/go-mutesting
[gms-releases]: https://github.com/avito-tech/go-mutesting/releases
[mu-readme]: https://github.com/mizchi/mutator
[mu-coverage]: https://github.com/mizchi/mutator/blob/main/docs/coverage-and-mutation.md
[mu-releases]: https://github.com/mizchi/mutator/releases
[st-switching]: https://stryker-mutator.io/blog/announcing-stryker-4-mutation-switching/
[st-config]: https://stryker-mutator.io/docs/stryker-js/configuration/
[st-thresholds]: https://stryker-mutator.io/docs/stryker-js/configuration/#thresholds-object
[st-usage]: https://stryker-mutator.io/docs/stryker-js/usage/
[st-incremental]: https://stryker-mutator.io/docs/stryker-js/incremental/
[st-checker]: https://stryker-mutator.io/docs/stryker-js/typescript-checker/
[st-disable]: https://stryker-mutator.io/docs/stryker-js/disable-mutants/
[st-plugins]: https://stryker-mutator.io/docs/stryker-js/plugins/
[st-states]: https://stryker-mutator.io/docs/mutation-testing-elements/mutant-states-and-metrics/
[st-mutators]: https://stryker-mutator.io/docs/mutation-testing-elements/supported-mutators/
[st-equivalent]: https://stryker-mutator.io/docs/mutation-testing-elements/equivalent-mutants/
[st-releases]: https://github.com/stryker-mutator/stryker-js/releases
[pit-concepts]: https://pitest.org/quickstart/basic_concepts/
[pit-mutators]: https://pitest.org/quickstart/mutators/
[pit-maven]: https://pitest.org/quickstart/maven/
[pit-incremental]: https://pitest.org/quickstart/incremental_analysis/
[pit-faq]: https://pitest.org/faq/
[pit-releases]: https://github.com/hcoles/pitest/releases
[cm-print]: https://mutants.rs/print.html
[cm-mutants]: https://mutants.rs/mutants.html
[cm-attrs]: https://mutants.rs/attrs.html
[cm-diff]: https://mutants.rs/in-diff.html
[cm-iterate]: https://mutants.rs/iterate.html
[cm-shards]: https://mutants.rs/shards.html
[cm-workspaces]: https://mutants.rs/workspaces.html
[cm-out]: https://mutants.rs/mutants-out.html
[cm-exit]: https://mutants.rs/exit-codes.html
[cm-timeouts]: https://mutants.rs/timeouts.html
[cm-select]: https://mutants.rs/print.html#selecting-tests-to-run
[cm-outcomes]: https://mutants.rs/print.html#mutant-outcomes
[cm-list]: https://mutants.rs/print.html#listing-generated-mutants
[cm-filter]: https://mutants.rs/print.html#filtering-functions-and-mutants
[cm-config]: https://mutants.rs/print.html#config-file
[cm-filter-config]: https://mutants.rs/print.html#configuring-filters-by-name
[cm-releases]: https://github.com/sourcefrog/cargo-mutants/releases
[mm-docs]: https://mutmut.readthedocs.io/en/latest/
[mm-arch]: https://github.com/boxed/mutmut/blob/main/ARCHITECTURE.rst
[mm-history]: https://github.com/boxed/mutmut/blob/main/HISTORY.rst
[mm-pypi]: https://pypi.org/project/mutmut/
