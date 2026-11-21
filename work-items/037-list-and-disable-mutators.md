---
status: done
stage: 3
depends_on: []
created_at: 2026-11-21
---

# 変異演算子の一覧を出し、演算子を個別に無効にできるようにする

## 動機

使える変異演算子は README を読むまで分からず、名前と変異の例を CLI から確かめられない。
演算子を選ぶ方法は `-only` だけで、1 つだけを外すには残りをすべて並べる必要がある。
[018](018-more-mutators.md) で演算子が増えると、どちらの不便も大きくなる。

## 対象範囲

- `-list-mutators` で、各演算子の名前、説明、変異の例、既定で有効かを出して終わる。
- `-disable` で、カンマ区切りの演算子を既定の集合から外す。`-only` と同時には指定できない（終了コード 1）。

## 対象外

- 設定ファイルでの指定。[010](010-config-file.md) が扱う。

## 設計

演算子の名前、説明、例は `internal/mutant` の演算子の定義に置き、README の一覧と `-list-mutators` の出力は同じ定義から作る。

## 完了の条件

- `-list-mutators` の出力に、すべての演算子が既定の有効と無効とともに並ぶ。
- `-disable` で外した演算子の変異体が列挙されない。

## 検証

- 引数の解釈の単体テストと、`-disable` を付けた端から端までのテスト。

## 要約

- 演算子の説明と例を `mutant.Infos`（`internal/mutant/info.go`）に 1 か所で定めた。`-list-mutators` は、名前、既定（`default`）か既定で無効（`opt-in`）か、説明、例を表にして出し、パッケージの指定なしで終わる。
- README の一覧は自動では作らず、全演算子の名前を挙げることを単体テスト（`TestInfos_MatchTheReadme`）で確かめる形にした。README は日本語の説明を持ち、`-list-mutators` の英語の説明と文が違うためである。
- `-disable` は既定の集合から演算子を外す。`-only` との併用と未知の名前は終了コード 1 にした（014 で引数の誤りを 1 にしたので、作業項目の「終了コード 2」から変わった）。
- 端から端までのテスト（`TestListMutators`、`TestDisable_RemovesOperatorsFromTheDefaultSet`）で、一覧の行と、外した演算子の変異体が列挙されないことを表明した。
