---
status: done
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

## 要約

- 10 演算子を既定で無効な演算子として足した。名前は着手前の表のとおりで、gomutants 0.7.3 の `-list-mutators` の名前と一致する。
- `REMOVE_SELF_ASSIGNMENTS` は複合代入の規則に `=` への置き換えを足しただけで、埋め込みは `INVERT_ASSIGNMENTS` と同じである。
- 数値のリテラルは、10 進で書き、負なら括弧で囲む（`a-0` を `a--1` にしない）。浮動小数点数は小数点か指数を必ず含め、整数の定数にしない。`make` の容量と `map` の大きさの見込みは、変えても振る舞いが変わらないので変異しない。
- 実在のパッケージで、`const maxBytes = 64 << 10` の `10` を呼び出しにすると、定数でないという誤りが箇所の外に出て、schemata の書き換え全体が止まった。`const` の宣言と配列の長さの中の箇所は、書き換える前に個別ビルドへ回すようにした（`schemata.constantContexts`）。リテラルの箇所は、`gamutMark` で包むと既定の型になるので `Marked` を飛ばす。
- `BRANCH_*` は本体を `if false { ... }` で包み、`fallthrough` で終わる節と、中身がすべて arid の本体は対象にしない。`RETURN_ERROR_NIL` は、式でだけ使う import が使われなくならないよう、置き換えに元の式を評価しない分岐で残す。結果の型が `error` の位置だけを変異し、schemata では `error` として受けて型付きの nil も元と同じに扱う。
- `fmt.Errorf` の呼び出しは arid（メッセージ）だが、`ERRORF_WRAP` と `RETURN_ERROR_NIL` はエラーの値を変えるので、その呼び出し自身の arid では止めない。
- idmagic の `usecases` と、`%w` の多い 5 パッケージで測り、10 演算子とも既定に入れないと決めた（理由は[設計の既定の集合](../docs/design.md#既定の集合)）。既定の集合の判定は、変更前と 2 パッケージ 1,223 体で一致した。
- 文字列のリテラルは [018](018-more-mutators.md) の対象外の理由のまま採らない。
