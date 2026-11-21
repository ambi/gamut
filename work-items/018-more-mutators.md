---
status: in_progress
stage: 3
depends_on: [010]
created_at: 2026-11-20
---

# 変異演算子を広げる

## 動機

gamut の 5 種類は演算子の置き換えに限られる。
条件の真偽への置き換え、呼び出し文の削除、戻り値の置き換えなど、コードの削除に当たる変異は、テストの見落としをよく見つける。
これらは [022](022-simplify-reporter.md) の前提でもある。

## 対象範囲

次を、既定で無効な演算子として足し、測ってから既定の集合を決める。

| 名前 | 変異 | 埋め込み方 |
| --- | --- | --- |
| `INVERT_LOGICAL` | `&&`↔`\|\|` | 短絡評価を保つため、関数リテラルの中で元の形と変異後の形を分岐させる |
| `CONDITIONS_CONSTANT` | `if`、`for` の条件と `&&`、`\|\|` の被演算子を `true` か `false` にする。`\|\|` の下では `false`、`&&` の下では `true` だけ | 条件を関数リテラルで包む |
| `STATEMENT_REMOVE` | 値を返さない呼び出しの文を削除する | 文を `if gamutActive != N { ... }` で包む |
| `RETURN_ZERO` | 関数の本体を、戻り値の型のゼロ値（`error` は `errors.New("gamut")`）を返す文にする | 本体の先頭に `if gamutActive == N { return ... }` を置く |
| `INVERT_ASSIGNMENTS` | `+=`↔`-=`、`*=`↔`/=`、`%=`→`*=` | 文を分岐させる |
| `INVERT_LOOP_CTRL` | `break`↔`continue` | 文を分岐させる |
| `REMOVE_LOGICAL_NOT` | `!x`→`x` | 式を包む |
| `BOOLEAN_LITERAL` | `true`↔`false` | 式を包む。定数の文脈（`const` の宣言など）は個別ビルドにする |
| `INVERT_BITWISE` | `&`↔`\|`、`^`→`&`、`&^`→`&`、`<<`↔`>>` | 二項演算子と同じく分岐させる |
| `INVERT_BITWISE_ASSIGNMENTS` | `&=`↔`\|=`、`^=`→`&=`、`&^=`→`&=`、`<<=`↔`>>=` | 文を分岐させる |

- 名前は、gomutants に同じ演算子があればそれに合わせる（着手時に gomutants 0.7.3 の `-list-mutators` で確かめ、仮の名前を付け直した）。
- `switch` の `case` の削除は、`default` があるときだけ行う。
- 等価になりやすい変異（`==`→`<=` など）は作らない。

## 対象外

- 文字列リテラルの置き換え。ログとエラー文の変異が多く、arid の規則が育つまで仕分けの負担が大きい。

## 設計

`STATEMENT_REMOVE` と `RETURN_ZERO` は字句 1 つの置き換えではないので、変異体の表現を「元の区間と置き換え後の字句」に広げる。
構築可能性の判定と個別ビルドはこの表現の上でそのまま動く。
`RETURN_ZERO` のゼロ値は型情報から作り、書けない型は除く。

## 完了の条件

- 各演算子に、変異する例、しない例、`KILLED` と `LIVED` になる例の固定データがある（`add-mutator` スキル）。
- idmagic の代表パッケージで、演算子ごとの生き残りの数と仕分けの内訳を測り、既定の集合を設計文書に書く。

## 検証

- 演算子ごとの単体テストと端から端までのテスト。

## 経過

演算子のまとまりごとにコミットする。

- 済み：既定で無効な演算子の仕組み（`mutant.OptIn` と、名前を確かめる `mutant.Known`）、`INVERT_BITWISE`、`INVERT_ASSIGNMENTS`、`INVERT_BITWISE_ASSIGNMENTS`、`INVERT_LOOP_CTRL`、`INVERT_LOGICAL`、`REMOVE_LOGICAL_NOT`、`BOOLEAN_LITERAL`。変異体の表現を「区間と置き換え後の文字列」に広げた（`OriginalText` と `ReplacementText`。演算子の変異の識別子は変わらない）。固定モジュール `testdata/operators` に、演算子ごとの `KILLED` と `LIVED` の例を足していく。
- 残り：`CONDITIONS_CONSTANT`、`STATEMENT_REMOVE`、`RETURN_ZERO`、idmagic での測定と既定の集合の決定。
