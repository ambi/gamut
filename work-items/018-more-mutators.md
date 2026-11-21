---
status: done
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

## 要約

- 10 の演算子を足した。名前は着手時に gomutants 0.7.3 の `-list-mutators` で確かめ、同じ演算子があればそれに合わせた（`INVERT_ASSIGNMENTS`、`INVERT_BITWISE_ASSIGNMENTS`、`INVERT_LOGICAL`、`INVERT_LOOP_CTRL`、`REMOVE_LOGICAL_NOT`、`STATEMENT_REMOVE`、`RETURN_ZERO`）。
- 変異体の表現を「区間と置き換え後の文字列」（`OriginalText`、`ReplacementText`）に広げた。識別子は置き換え後の文字列から作るので、演算子の変異の識別子と既存のキャッシュは変わらない。文や型を含む変異（`STATEMENT_REMOVE`、`RETURN_ZERO`）は、識別子に固定の文字列を使う。
- 埋め込み方：
  - 短絡評価の `&&` と `||` は、右辺を関数リテラルの中に 1 回だけ書いて必要なときだけ呼ぶ。
  - `break` と `continue` は、文を 2 つの分岐にする。
  - 複合代入は、右辺を 1 回だけ評価して演算子を切り替える。
  - 条件と関数の本体は、独自の箇所のノード（`ConditionSite`、`BodySite`）で包む。
  - 書き換え後の型検査で落ちる箇所（ループの終端性が変わる、`const` の中など）は、個別ビルドに回る。
- 対象範囲からの変更：`RETURN_ZERO` の `error` は `errors.New("gamut")` ではなく `nil` にした。`nil` でない値には import が要り、字句の置き換えだけで構築可能性を判定できなくなるためである。`for` の条件の `CONDITIONS_CONSTANT` は、無限ループになる `true` を作らず `false` だけにした。
- 演算子ごとに、変異する例としない例の表の単体テストと、`testdata/operators` の `KILLED` と `LIVED` の例の端から端までのテストを置いた。
- idmagic の `usecases` で測り、`RETURN_ZERO` と `STATEMENT_REMOVE` を既定に入れた（時間は 1〜2 割増し、増えた生き残り 16 体はどれもテストの穴）。ほかは既定で無効のままにした。演算子ごとの内訳と理由は設計の「既定の集合」に書いた。
- 既定の集合を変えたので、判定の仕組みを確かめる既存のテストは、前の 5 演算子（`classic`）に固定した。
- 途中で、パッケージ変数をためるテストが、使い回すテストプロセスの中で誤った `LIVED` を生むことが分かり、038 として起票した。
