# 作業項目

この文書は、gamut の作業項目の形式と、ゴールまでの一覧を定める。

## ゴール

Go のパッケージのミューテーションテストを、日々の開発と CI で繰り返し回せるツールにする。
次の 3 つを満たしたときにゴールに達したとみなす。

- [設計のロードマップの表](../docs/design.md#目標とロードマップ)の機能をすべて持つ。
- 公開されている Go のプロジェクト数件で、初回、変更なしの再実行、1 関数を変えた後の再実行の時間を測り、結果を公開する（[025](025-benchmark-on-public-projects.md)）。
- `go install` で入れられる版を公開している（[024](024-release-and-distribution.md)）。

## 形式

ファイル名は `<3 桁の番号>-<ケバブケースの題名>.md` とする。
番号は起票順に振り、着手の順序は `depends_on` で表す。

```markdown
---
status: pending        # pending、in_progress、done、cancelled
stage: 2               # 設計のロードマップの段階
depends_on: []         # 先に終える項目の番号
created_at: 2026-11-20
---

# 結果を表す一文

## 動機
## 対象範囲
## 対象外
## 設計
## 完了の条件
## 検証
```

- **動機**には、今の gamut の何が足りないかを書く。
- **設計**には、採る方式と採らない案を書く。作るものを変え得る問いは着手前に解決する。
- **完了の条件**には、観測できる結果を書く。テストで確かめられる形にする。
- 完了したら `status: done` にし、末尾に `## 要約` を書いて、後から読む人が知るべき判断を数行で残す。項目のファイルは消さない。
- 設計の判断のうち完了後も有効なものは、[docs/design.md](../docs/design.md) へ反映する。

## 一覧

| 番号 | 段階 | 題名 | 依存 |
| --- | --- | --- | --- |
| [001](001-forced-fail-self-check.md) | 2 | 変異がテストに届くことを判定の前に自己検査する | |
| [002](002-hardened-test-execution.md) | 2 | テストの実行を堅牢にする（ビルドタグ、テストのフラグ、プロセスグループ） | |
| [003](003-multi-package-runs.md) | 2 | 複数のパッケージを 1 回の実行で判定する | |
| [004](004-result-cache.md) | 2 | 判定を位置に依存しない鍵でキャッシュし、変わらない変異体を実行しない | 003 |
| [005](005-incremental-coverage.md) | 2 | 被覆の記録を、影響を受けるテストだけやり直す | 004 |
| [006](006-killer-first-ordering.md) | 2 | 前回の検出テストを先に実行する | 004 |
| [007](007-since-diff-mode.md) | 2 | `--since` で差分の変異体だけを実行して報告する | 004 |
| [008](008-rerun-single-mutant.md) | 2 | 変異体を識別子で指定して再実行する | 004 |
| [009](009-integration-mode.md) | 2 | 逆依存パッケージのテストでも検出を試す | 003 |
| [010](010-config-file.md) | 3 | 設定ファイルを読む | |
| [011](011-disable-comments.md) | 3 | 無効化コメントで変異を止める | |
| [012](012-accept-equivalent-mutants.md) | 3 | 等価な変異体をソースの外に受理として記録する | 004 |
| [013](013-equivalence-detection.md) | 3 | 等価な変異体をコンパイル結果の比較で自動検出する | |
| [014](014-critical-regions-thresholds-exit-codes.md) | 3 | 重要領域、閾値、終了コードを定める | 012 |
| [015](015-mutation-testing-elements-reports.md) | 3 | mutation-testing-elements の JSON と HTML を出す | |
| [016](016-github-annotations-and-action.md) | 3 | GitHub の注釈と Action を用意する | 007、014 |
| [017](017-agent-reporter-and-skill.md) | 3 | エージェント向けの報告と skill を用意する | 008 |
| [018](018-more-mutators.md) | 3 | 変異演算子を広げる | 010 |
| [019](019-arid-rules.md) | 3 | arid の規則を広げ、設定で足せるようにする | 010 |
| [020](020-select-tests.md) | 4 | 変更が壊しうるテストを選んで出力する | 004 |
| [021](021-kill-matrix.md) | 4 | kill matrix で検出しないテストと包含されるテストを報告する | 004 |
| [022](022-simplify-reporter.md) | 4 | 不要かもしれないコードを報告する | 018 |
| [023](023-stack-depth-limit.md) | 4 | 呼び出しの深さで被覆を絞る | |
| [024](024-release-and-distribution.md) | 公開 | 版を公開し、`go install` で入れられるようにする | 001、002 |
| [025](025-benchmark-on-public-projects.md) | 公開 | 公開プロジェクトで gomutants と Gremlins と比べる | 004、007 |
| [026](026-sharding-and-time-budget.md) | 4 | CI のシャードと時間予算を持つ | 004 |
| [027](027-watch-mode.md) | 4 | 保存のたびに差分を判定する | 007 |
| [028](028-fast-unchanged-rerun.md) | 2 | 変更のない再実行を 1 秒未満にする | 005 |
| [029](029-single-process-coverage.md) | 2 | 被覆の記録を、テストバイナリごとに 1 つのプロセスで行う | 005 |
| [030](030-reuse-test-processes.md) | 2 | 変異体の判定に、テストプロセスを使い回す | 006、029 |
| [031](031-fast-rerun-after-edit.md) | 2 | 1 関数を変えた後の再実行を短くする | 005、007、030 |
| [032](032-batch-revdep-coverage.md) | 2 | 統合モードで、逆依存パッケージのテストの被覆をまとめて測る（取りやめ） | 009、029 |
| [033](033-windows-support.md) | 公開 | Windows で判定が macOS と Linux と一致することを確かめる | 002、030 |
| [034](034-cgo-packages.md) | 3 | cgo を使うパッケージを判定する | |
| [035](035-comparison-matrix.md) | 公開 | README に、主要なミューテーションテストツールとの機能の比較表を置く | 024 |
| [036](036-checkpoint-cache.md) | 3 | 実行中の判定を、一定の間隔でキャッシュに書き出す | 004 |
| [037](037-list-and-disable-mutators.md) | 3 | 変異演算子の一覧を出し、演算子を個別に無効にできるようにする | |
| [038](038-detect-unrepeatable-tests.md) | 3 | 繰り返し実行できないテストを見つけ、テストプロセスを使い回さずに判定する | 030 |
