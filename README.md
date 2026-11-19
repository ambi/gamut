# gamut

gamut は Go 向けのミューテーションテストツールである。
パッケージのソースに機械的な変異（`<` を `<=` にするなど）を加え、テストがその変異を検出できるかを調べる。

- **速い**：全変異体を 1 回のビルドに埋め込み（mutant schemata）、どれを有効にするかを実行時に選ぶ。変異体ごとにコンパイルしない。
- **ビルドキャッシュを汚さない**：変異体ごとのビルド結果を共有の Go ビルドキャッシュへ書かない。
- **生き残りを仕分ける**：テストが到達しなかった（`NOT COVERED`）、到達したが値が変わらなかった（`LIVED`、`not infected`）、値は変わったが失敗しなかった（`LIVED`、`not detected`）に分ける。直し方がそれぞれ違うためである。
- **依存がない**：Go の標準ライブラリと `go` コマンドだけを使う。

設計は [docs/design.md](docs/design.md) が定める。

## 使い方

```console
go build -o bin/gamut ./cmd/gamut
./bin/gamut -o report.json ./path/to/package
```

| フラグ | 既定値 | 意味 |
| --- | --- | --- |
| `-workers` | 2 | 並列に動かすテストプロセスの数 |
| `-only` | すべて | 有効にする変異演算子（カンマ区切り） |
| `-o` | なし | JSON の報告の書き出し先 |
| `-dry-run` | false | 変異体と構築可能性だけを一覧にする |
| `-timeout-factor` | 5 | 選んだテストの基準の時間に掛ける係数 |
| `-timeout-min` | 2s | 時間切れの上限に足す時間 |
| `-q` | false | 進行状況を出さない |

変異演算子は `ARITHMETIC_BASE`、`CONDITIONALS_BOUNDARY`、`CONDITIONALS_NEGATION`、`INCREMENT_DECREMENT`、`INVERT_NEGATIVES` で、Gremlins と gomutants と同じ名前である。
判定の前に、変異がテストに届くことを 1 件のテストで確かめる（forced fail）。
届いていなければ判定せずに終了コード 4 で止まる。

JSON の報告は gomutants と同じ `files[].mutations[]` の形に、`reason`、`mode`、`tests`、`killed_by` を足したものである。

## 測定

2026-11-20、Apple Silicon の macOS、Go 1.27.4。
対象は idmagic の `backend/oauth2/internal/token/usecases`（変異体 562〜563 体、テスト 135 件）で、同じ 5 種類の変異演算子、ワーカー数 2 で実行した。

| 項目 | gomutants 0.7.3（`--cache off`） | gamut |
| --- | --- | --- |
| 実行時間 | 352 秒 | 38 秒 |
| 共有ビルドキャッシュの増加 | 6,197 ファイル、2.6 GiB | 142 ファイル、30 MiB（開発中の版の初回。同じソースでの再実行では増えない） |
| 判定 | killed 439、lived 33、not covered 84、not viable 7 | killed 438、lived 29、not covered 85、not viable 10 |

gamut の内訳は、読み込みと構築可能性の判定 1.6 秒、schemata のビルド 0.9 秒、被覆の記録 7.4 秒、変異体の実行が残りである。
563 体のうち 550 体を schemata に埋め込み、2 体（定数宣言）を個別にビルドした。

両者が生成した変異体の判定の食い違いは 12 件で、すべて原因を確かめた。

| 件数 | 原因 | 正しい判定 |
| --- | --- | --- |
| 6 | `a != "" && a != X` の右辺など、短絡評価で評価されない式。gomutants は行単位の被覆で `LIVED`、gamut は式単位で `NOT COVERED` | gamut |
| 3 | 文字列の `+` を `-` にする変異。gomutants は被覆を先に見て `NOT COVERED`、gamut は型検査を先にして `NOT VIABLE` | どちらも妥当 |
| 2 | 定数宣言（`30 * time.Second`）の変異。gomutants は行が実行されないので `NOT COVERED`、gamut は個別ビルドで全テストを実行して `LIVED` | gamut（定数の値はテストから到達しうる） |
| 1 | gomutants が二項の `-` にも `INVERT_NEGATIVES` を付けている | gamut（単項の `-` だけが対象） |

## 開発

コマンドは `mise run <task>` で実行する（[AGENTS.md](AGENTS.md)）。

```console
mise run verify
```
