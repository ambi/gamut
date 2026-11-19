---
name: compare-with-gomutants
description: 実在の Go パッケージで gamut と gomutants の判定、実行時間、ビルドキャッシュの増加を比べる。判定の互換性を確かめて、gomutants と比較して、ベンチマークを取って、と頼まれたときに使う。
---

# gomutants との比較

比較は、gomutants の判定を正としてではなく、食い違いの原因を一件ずつ説明するために行う。

1. 対象のリポジトリとパッケージディレクトリを決める。gomutants のバージョンを `gomutants -version` で記録する。
2. 両方を同じ条件で実行する。変異演算子は `ARITHMETIC_BASE,CONDITIONALS_BOUNDARY,CONDITIONALS_NEGATION,INCREMENT_DECREMENT,INVERT_NEGATIVES`、ワーカー数は 2、gomutants のキャッシュは `--cache off` にする。
3. 各実行の前後で `go env GOCACHE` 配下のファイル数と容量（`du -sk`）を測り、実行時間とともに記録する。
4. 報告の `files[].mutations[]` を、ファイル名、行、列、変異演算子、置き換え後の字句で突き合わせる（識別子は両者で形が違う）。`jq` で両方を `<file>:<line>:<column>:<type>:<replacement>\t<status>` の行に直し、`join` で並べる。
5. 状態が食い違う変異体ごとに、変異後のソースとテストを読み、どちらの判定が正しいかと理由を書く。gamut の `LIVED`（`reason` が `not infected` を含む）は gomutants の `LIVED` に対応させる。
6. gamut の誤りであれば、`testdata/` に再現する固定モジュールを足してから直す。
7. 結果を README の測定の表に、日付、対象、バージョン、実行時間、キャッシュの増加、食い違いの件数と内訳とともに書く。
