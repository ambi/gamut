# 変更履歴

gamut の版ごとの変更を記録する。
版は [Semantic Versioning](https://semver.org/lang/ja/) に従う。
1.0.0 までは、マイナー版で報告の形と変異体の識別子の計算を変えうる。

次の変更は、利用者の記録（受理の記録、キャッシュ、報告を読むスクリプト）に影響するので、必ず理由とともに書く。

- 変異体の識別子の計算の変更。`gamut.accepted.json` の受理と、キャッシュの判定が引き継がれなくなる。
- 報告の形の変更（フィールドの追加を除く）と、判定の状態の名前の変更。
- 設定ファイル `gamut.json` の項目の変更と削除。
- 既定の変異演算子の集合の変更。点数と生き残りの数が変わる。

## 未公開

## [0.1.0] - 2026-11-21

最初の版。

- 全変異体を 1 回のビルドに埋め込む（mutant schemata）。変異体ごとのビルド結果を共有の Go ビルドキャッシュへ書かない。
- 式単位の被覆と感染の記録で、生き残りを `NOT COVERED`、`LIVED`（`not infected`）、`LIVED`（`not detected`）に分ける。
- 変異演算子は 25 種類で、既定の集合は `ARITHMETIC_BASE`、`CONDITIONALS_BOUNDARY`、`CONDITIONALS_NEGATION`、`INCREMENT_DECREMENT`、`INVERT_NEGATIVES`、`RETURN_ZERO`、`STATEMENT_REMOVE` の 7 種類。
- 判定と被覆の記録の再利用、差分だけの判定（`-since`）、変異体の再実行（`-mutant`）、逆依存パッケージのテストでの検出（`-integration`）、保存のたびの判定（`-watch`）。
- 無効化コメント、等価な変異体の受理（`gamut accept`）と自動検出（`-detect-equivalent`）、重要領域、閾値と終了コード。
- 報告：gomutants 互換の JSON、mutation-testing-elements の JSON と HTML、エージェント向けの JSON、GitHub の注釈と Action。
- `gamut select-tests`、`-kill-matrix`、`-reporter simplify`、`-max-depth`、`-shard` と `gamut merge`、`-time-budget`。
- macOS と Linux で動作を確かめた。Windows は確かめていない。

[0.1.0]: https://github.com/ambi/gamut/releases/tag/v0.1.0
