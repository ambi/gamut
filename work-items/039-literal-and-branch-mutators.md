---
status: pending
stage: 3
depends_on: [018]
created_at: 2026-11-21
---

# 数値のリテラル、分岐の本体、エラーの包みを変える変異演算子を足す

## 動機

gamut の変異演算子は、数値のリテラル、`if` と `case` の本体、`fmt.Errorf` の `%w` を変えない。
そのため、定数の境界の誤り（`maxRetries = 3` を `4` にしても失敗しない）、分岐の中身を確かめないテスト、`errors.Is` で確かめていないエラーの包みを見落とす。
[035](035-comparison-matrix.md) の比較表で、これらがほかの演算子の生き残りと重ならないテストの穴であることが分かった。

## 対象範囲

次を、既定で無効な演算子として足し、[018](018-more-mutators.md) と同じく実在のパッケージで生き残りの数と仕分けの内訳を測ってから、既定の集合に入れるかを決める。

| 仮の名前 | 変異 |
| --- | --- |
| `INTEGER_INCREMENT`、`INTEGER_DECREMENT` | 整数のリテラルを 1 増やす、1 減らす |
| `FLOAT_INCREMENT`、`FLOAT_DECREMENT` | 浮動小数点数のリテラルを 1 増やす、1 減らす |
| `BRANCH_IF`、`BRANCH_ELSE`、`BRANCH_CASE` | `if`、`else`、`case` の本体を空にする |
| `ERRORF_WRAP` | `fmt.Errorf` の `%w` を `%v` にする |
| `RETURN_ERROR_NIL` | 返す `error` を `nil` にする |
| `REMOVE_SELF_ASSIGNMENTS` | `x += y` を `x = y` にする |

- 名前は、Gremlins と gomutants に同じ演算子があれば着手時にそれに合わせる。
- 定数の文脈（`const` の宣言、配列の長さ）のリテラルは、`BOOLEAN_LITERAL` と同じく個別ビルドに回す。

## 対象外

- 文字列のリテラルの置き換え。ログとエラー文の変異が多く、仕分けの負担が大きい（[018](018-more-mutators.md) の対象外と同じ理由）。

## 設計

着手時に、埋め込み方（関数リテラルで包むか、文を分岐させるか）と、識別子の置き換え後の部分を演算子ごとに決める。
`BRANCH_*` は、本体を空にした後に使われなくなる変数で構築不能になりうるので、`STATEMENT_REMOVE` と同じく `if false { ... }` で参照を残す。

## 完了の条件

- 各演算子に、変異する例、しない例、`KILLED` と `LIVED` になる例の固定データがある（`add-mutator` スキル）。
- `-list-mutators` と README の一覧に各演算子が出る。
- 実在のパッケージで測った内訳と、既定の集合に入れるかの判断が [docs/design.md](../docs/design.md#既定の集合) にある。

## 検証

- `mise run verify`
