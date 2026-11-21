# 機能の比較

主要なミューテーションテストツールの機能を、2026-11-21 に各ツールの公式の文書と `--help` で確かめて並べた。
各セルはその出典へのリンクである。
「ある」「一部」「ない」は文書に書かれていることだけで判断し、文書に書かれていないものは「不明」とした。
フラグや設定の一覧が載った文書にない機能は「ない」とした。
「—」は、言語の違いで比べられない観点である。
gamut の「一部」「ない」には、まだ確かめていないことか、採らない理由を書いた。
速さの比較は[測定](benchmarks.md)が扱う。

| 観点 | gamut | gomutants | Gremlins | go-mutesting | mutator | StrykerJS | PIT | cargo-mutants | mutmut |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 変異の埋め込み方 | [schemata（1 回のビルド）][g-schemata] | [変異体ごとにビルド（overlay）][gm-how] | [不明][gr-workers] | [変異体ごとにファイルを置き換えて実行][gms-readme] | [schemata][mu-readme] | [schemata][st-switching] | [変異体ごとにバイトコードをメモリ上で書き換え][pit-concepts] | [変異体ごとにビルド（木の複製）][cm-workspaces] | [schemata][mm-arch] |
| 被覆によるテストの選択 | [ある（テストごと、式単位）][g-infection] | [ある（テストごと、行単位）][gm-how] | [一部（被覆のない変異体を飛ばす。テストはパッケージ単位）][gr-unleash] | [ない（パッケージのテストをすべて実行）][gms-readme] | [ある（テストごと）][mu-readme] | [ある（テストごと）][st-config] | [ある（テストごと、行単位）][pit-faq] | [ない（パッケージのテストを実行）][cm-select] | [ある（テストごと）][mm-arch] |
| 生き残りの分類（未到達、未感染、未検出） | [ある（3 分類）][g-report] | [一部（未到達と生き残り）][gm-mutators] | [一部（未到達と生き残り）][gr-quick] | [ない（検出と生き残り）][gms-readme] | [ある（3 分類）][mu-coverage] | [一部（未到達と生き残り）][st-states] | [一部（未到達と生き残り）][pit-concepts] | [ない（生き残り）][cm-outcomes] | [不明][mm-arch] |
| 構築不能の事前判定 | [ある（`go/types`）][g-roadmap] | [ない（ビルドの失敗で判定）][gm-how] | [不明（ビルドの失敗を NOT VIABLE にする）][gr-quick] | [不明][gms-readme] | [ある（型検査）][mu-readme] | [ある（TypeScript の型検査、任意）][st-checker] | [ない（JVM が読み込めない変異体を実行時に判定）][pit-concepts] | [一部（`--check` で `cargo check` だけを実行）][cm-list] | [ある（mypy、pyrefly、任意）][mm-docs] |
| 結果の再利用 | [ある（宣言と到達したテストの単位）][g-reuse] | [ある（パッケージとテストの単位）][gm-how] | [ない][gr-unleash] | [不明][gms-readme] | [ある（囲む関数の単位）][mu-readme] | [ある（`--incremental`）][st-incremental] | [ある（履歴ファイル、実験的）][pit-incremental] | [一部（`--iterate` で前回の検出を飛ばす）][cm-iterate] | [ある（関数の単位）][mm-history] |
| 差分の実行 | [ある（`-since`）][g-since] | [ある（`--changed-since`）][gm-pr] | [ある（`--diff`）][gr-unleash] | [不明][gms-readme] | [ある（`--since`）][mu-readme] | [ない][st-incremental] | [ない（有償の Arcmutate にある）][pit-maven] | [ある（`--in-diff`）][cm-diff] | [ない][mm-docs] |
| 変異体の再実行 | [ある（`-mutant`）][g-mutant] | [ある（`--run-mutant-id`）][gm-rerun] | [ない][gr-unleash] | [不明][gms-readme] | [ある（`--only`）][mu-readme] | [一部（行の範囲で選ぶ）][st-incremental] | [ない][pit-maven] | [一部（名前の正規表現で選ぶ）][cm-filter] | [一部（名前のワイルドカードで選ぶ）][mm-docs] |
| ほかのパッケージのテストで検出 | [ある（逆依存、被覆で選ぶ）][g-integration] | [ある（逆依存、被覆で選ぶ）][gm-cross] | [一部（`--integration` で全テストを実行）][gr-unleash] | [不明][gms-readme] | [不明][mu-readme] | [不明][st-config] | [一部（`crossModule`、限定的）][pit-maven] | [ある（`--test-workspace`）][cm-workspaces] | [不明][mm-docs] |
| 変異演算子の数 | [25（既定 7）][g-mutators] | [28][gm-features] | [11（既定 5）][gr-config] | [15 群][gms-readme] | [18][mu-readme] | [16 群][st-mutators] | [29（既定 11）][pit-mutators] | [6 種類][cm-mutants] | [不明][mm-docs] |
| 戻り値の置き換えと文の削除 | [ある][g-mutators] | [ある][gm-mutators] | [ない][gr-config] | [ある][gms-readme] | [ある][mu-readme] | [ある][st-mutators] | [ある][pit-mutators] | [ある][cm-mutants] | [不明][mm-docs] |
| 数値と文字列のリテラルの変異 | [一部（数値）][g-mutators]（文字列は[採らない](../design/mutants.md#採らなかった案)） | [一部（数値）][gm-mutators] | [ない][gr-config] | [一部（数値）][gms-readme] | [一部（数値、プラグイン）][mu-readme] | [ある][st-mutators] | [ある][pit-mutators] | [不明][cm-mutants] | [ある][mm-docs] |
| 無効化コメント | [ある][g-ignore] | [ある][gm-ignore] | [不明][gr-config] | [ある][gms-readme] | [ある][mu-readme] | [ある][st-disable] | [ない（有償の Arcmutate にある）][pit-maven] | [ある（属性）][cm-attrs] | [ある][mm-docs] |
| ソースの外での受理 | [ある（変異体ごと）][g-accept] | [ない][gm-help] | [ない][gr-config] | [ある（チェックサム）][gms-readme] | [ある（変異体ごと）][mu-readme] | [ない][st-disable] | [一部（メソッドとクラスの glob）][pit-maven] | [一部（名前の正規表現）][cm-filter-config] | [ない][mm-docs] |
| 等価な変異体の自動検出 | [ある（コンパイル結果の比較、任意）][g-equiv] | [ある（コンパイル結果の比較、任意）][gm-features] | [不明][gr-unleash] | [不明][gms-releases] | [一部（型で証明できる `?.`）][mu-coverage] | [ない][st-equivalent] | [一部（特定の変異を除く）][pit-mutators] | [不明][cm-mutants] | [ない][mm-history] |
| 設定ファイル | [ある（`gamut.json`）][g-config] | [ある（`.gomutants.yml`）][gm-config] | [ある（`.gremlins.yaml`）][gr-config] | [ある（YAML）][gms-readme] | [ある（`mutator.config.ts` など）][mu-readme] | [ある（`stryker.config.json` など）][st-usage] | [ある（Maven の設定）][pit-maven] | [ある（`.cargo/mutants.toml`）][cm-config] | [ある（`pyproject.toml` など）][mm-docs] |
| 閾値と終了コード | [ある（重要領域を含む）][g-quality] | [ある][gm-exit] | [ある][gr-unleash] | [不明][gms-readme] | [ある（重要領域を含む）][mu-readme] | [ある][st-thresholds] | [ある][pit-maven] | [一部（生き残りで終了コード 2。割合の閾値は一覧にない）][cm-exit] | [不明][mm-history] |
| mutation-testing-elements の JSON | [ある][g-formats] | [ある][gm-stryker] | [ない][gr-unleash] | [不明（独自の JSON）][gms-readme] | [ある][mu-readme] | [ある][st-plugins] | [ない（HTML、XML、CSV）][pit-maven] | [ない（独自の JSON）][cm-out] | [ない][mm-docs] |
| HTML の報告 | [ある][g-formats] | [ある][gm-html] | [ない][gr-unleash] | [ある][gms-readme] | [ある][mu-readme] | [ある][st-config] | [ある][pit-maven] | [不明][cm-out] | [ない][mm-docs] |
| CI の注釈 | [一部（GitHub。公開リポジトリの Pull Request では未確認）][g-github] | [ある（GitHub）][gm-features] | [ない][gr-action] | [不明][gms-readme] | [ある（GitHub）][mu-readme] | [ない][st-plugins] | [ない（有償の Arcmutate にある）][pit-maven] | [ある（GitHub）][cm-releases] | [ない][mm-docs] |
| エージェント向けの報告 | [ある（報告と skill）][g-agent] | [一部（Claude Code のプラグイン）][gm-plugin] | [ない][gr-unleash] | [不明][gms-readme] | [ある（報告と skill）][mu-readme] | [ない][st-plugins] | [ない][pit-maven] | [不明][cm-out] | [ない][mm-docs] |
| CI のシャード | [ある（まとめる `merge` を含む）][g-shard] | [ない][gm-compare] | [ない][gr-unleash] | [不明][gms-readme] | [ある][mu-readme] | [ない][st-config] | [ない][pit-maven] | [ある][cm-shards] | [ない][mm-docs] |
| 時間予算 | [ある][g-shard] | [ない][gm-help] | [ない][gr-unleash] | [不明][gms-readme] | [ある][mu-readme] | [ない][st-config] | [ない][pit-maven] | [不明][cm-timeouts] | [ない][mm-docs] |
| 保存のたびの再実行 | [ある][g-watch] | [ない][gm-help] | [ない][gr-unleash] | [不明][gms-readme] | [ある][mu-readme] | [ない][st-usage] | [ない][pit-maven] | [不明][cm-print] | [ない][mm-docs] |
| OS | [macOS、Linux（Windows は未確認）][g-exec] | [macOS、Linux（Windows は自動テストなし）][gm-install] | [Linux、macOS、Windows][gr-install] | [不明][gms-readme] | [Linux、Windows を CI で確認][mu-readme] | [不明][st-releases] | [不明][pit-faq] | [不明][cm-print] | [POSIX（Windows は WSL）][mm-docs] |
| cgo | [一部（`import "C"` のないファイル）][g-cgo] | [不明][gm-readme] | [不明][gr-install] | [不明][gms-readme] | — | — | — | — | — |

比べた版は次のとおりである。

- gamut：v0.1.0（比べたのはその直前のコミット e1fd6fd の機能で、その後に変異演算子を 10 種類足した）
- gomutants：[v0.7.3][gm-readme]
- Gremlins：[v0.6.0][gr-releases]（2025-12-06）
- go-mutesting：avito-tech の fork の [v2.3.1][gms-releases]（2025-12-26）。元の zimmski/go-mutesting より機能が多い
- mutator（TypeScript）：[v0.5.0][mu-releases]（2026-11-19）
- StrykerJS：[v10.1.1][st-releases]（2026-10-23）
- PIT：[1.30.2][pit-releases]（2026-11-05）。有償の拡張 Arcmutate は含めない
- cargo-mutants：[v27.1.0][cm-releases]（2026-06-02）
- mutmut：[3.8.1][mm-pypi]（2026-10-28）

出典は次のとおりである。

- gamut：[設計](../design/README.md)と[手引き](../../README.md#文書)
- gomutants：[README（v0.7.3）][gm-readme]と、`gomutants --help`（v0.7.3）
- Gremlins：<https://gremlins.dev/latest/>、<https://gremlins.dev/latest/quick-start/>、<https://gremlins.dev/latest/install/>、<https://gremlins.dev/latest/usage/commands/unleash/>、<https://gremlins.dev/latest/usage/commands/unleash/workers/>、<https://gremlins.dev/latest/usage/configuration/>、<https://gremlins.dev/latest/usage/ci/github-action/>、<https://github.com/go-gremlins/gremlins/releases>
- go-mutesting：<https://github.com/avito-tech/go-mutesting>、<https://github.com/avito-tech/go-mutesting/releases>
- mutator：<https://github.com/mizchi/mutator>、<https://github.com/mizchi/mutator/blob/main/docs/coverage-and-mutation.md>、<https://github.com/mizchi/mutator/releases>
- StrykerJS：<https://stryker-mutator.io/blog/announcing-stryker-4-mutation-switching/>、<https://stryker-mutator.io/docs/stryker-js/configuration/>、<https://stryker-mutator.io/docs/stryker-js/usage/>、<https://stryker-mutator.io/docs/stryker-js/incremental/>、<https://stryker-mutator.io/docs/stryker-js/typescript-checker/>、<https://stryker-mutator.io/docs/stryker-js/disable-mutants/>、<https://stryker-mutator.io/docs/stryker-js/plugins/>、<https://stryker-mutator.io/docs/mutation-testing-elements/mutant-states-and-metrics/>、<https://stryker-mutator.io/docs/mutation-testing-elements/supported-mutators/>、<https://stryker-mutator.io/docs/mutation-testing-elements/equivalent-mutants/>、<https://github.com/stryker-mutator/stryker-js/releases>
- PIT：<https://pitest.org/quickstart/basic_concepts/>、<https://pitest.org/quickstart/mutators/>、<https://pitest.org/quickstart/maven/>、<https://pitest.org/quickstart/incremental_analysis/>、<https://pitest.org/faq/>、<https://github.com/hcoles/pitest/releases>
- cargo-mutants：<https://mutants.rs/print.html>、<https://mutants.rs/mutants.html>、<https://mutants.rs/attrs.html>、<https://mutants.rs/in-diff.html>、<https://mutants.rs/iterate.html>、<https://mutants.rs/shards.html>、<https://mutants.rs/workspaces.html>、<https://mutants.rs/mutants-out.html>、<https://mutants.rs/exit-codes.html>、<https://mutants.rs/timeouts.html>、<https://github.com/sourcefrog/cargo-mutants/releases>
- mutmut：<https://mutmut.readthedocs.io/en/latest/>、<https://github.com/boxed/mutmut/blob/main/ARCHITECTURE.rst>、<https://github.com/boxed/mutmut/blob/main/HISTORY.rst>、<https://pypi.org/project/mutmut/>

[g-schemata]: ../design/execution.md#schemata-の書き換え
[g-infection]: ../design/execution.md#感染の記録
[g-report]: ../design/reporting.md#判定と報告
[g-roadmap]: ../design/README.md#目標とロードマップ
[g-reuse]: ../design/reuse.md#判定の再利用
[g-since]: ../design/selection.md#差分-since
[g-mutant]: ../design/selection.md#識別子-mutant
[g-integration]: ../design/selection.md#統合モード
[g-mutators]: ../design/mutants.md#変異演算子
[g-ignore]: ../design/mutants.md#無効化コメント
[g-accept]: ../design/mutants.md#等価な変異体の受理
[g-equiv]: ../design/mutants.md#等価な変異体の自動検出
[g-config]: ../design/reporting.md#設定ファイル
[g-quality]: ../design/reporting.md#品質の条件
[g-formats]: ../design/reporting.md#報告の形式
[g-github]: ../design/reporting.md#github-の注釈と-action
[g-agent]: ../design/reporting.md#エージェント向けの報告
[g-shard]: ../design/selection.md#シャードと時間予算
[g-watch]: ../design/selection.md#保存のたびの判定
[g-exec]: ../design/execution.md#テストバイナリの起動
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
