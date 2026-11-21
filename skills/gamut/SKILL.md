---
name: gamut
description: Go のミューテーションテストツール gamut を導入し、実行し、設定し、CI に組み込む。gamut を入れて、ミューテーションテストを回して、CI で品質の条件を付けて、と頼まれたときに使う。生き残りを直す作業は mutation-test-loop を使う。
---

# gamut の導入と実行

## 導入

```console
go install github.com/ambi/gamut/cmd/gamut@latest
```

gamut は、パッケージの全変異体を 1 回のビルドに埋め込み、変異体ごとに到達したテストだけを実行する。
判定と被覆の記録は作業ディレクトリの `.gamut/` に保存し、次の実行では変わった箇所だけをやり直す。
`.gamut/` はリポジトリに入れない（`.gitignore` に足す）。

## 実行

```console
gamut ./...                       # モジュール全体
gamut -since main ./...           # main からの差分に交わる変異体だけ
gamut -mutant '<id>' ./path/to/pkg  # 1 体だけ、キャッシュを使わずに
gamut -reporter agent ./path/to/pkg # エージェント向けの JSON を標準出力に
gamut -reporter html ./...        # .gamut/report/mutation.html
```

判定の状態は、`KILLED`（検出した）、`LIVED`（生き残り。`reason` が `not detected` か `not infected`）、`NOT COVERED`（どのテストも到達しない）、`NOT VIABLE`（型検査を通らない）、`TIMED OUT`、`PENDING`（`-since` の外）、`EQUIVALENT`（機械語が元と同じ）である。
生き残りを直すときは mutation-test-loop の skill に従う。

## 設定

モジュールのルートの `gamut.json` に置く。コマンドラインのフラグが優先する。未知の項目は終了コード 1 になる。

```json
{
  "exclude_files": ["_gen\\.go$"],
  "critical": ["internal/auth/**"],
  "thresholds": { "high": 80, "low": 60, "break": 70 },
  "test_flags": ["-short"]
}
```

- `critical` の glob に当たるファイルと、関数の宣言の Doc の `//gamut:critical` は重要領域で、受理していない生き残りがあれば終了コード 2 になる。
- `thresholds.break` は、点数（検出した変異体の、検出したものと生き残りの和に対する割合）の下限である。
- 観測できない箇所は、行に `//gamut:ignore [TYPE,...] -- <理由>` を書いて止める。どの入力でも区別できない変異体は、ソースを変えずに `gamut accept -reason '<理由>' '<id>'` で受理する。

## 終了コード

| コード | 意味 | 対処 |
| --- | --- | --- |
| 0 | 判定を終え、品質の条件を満たした | |
| 1 | 引数、設定、読み込み、ビルドの誤り | メッセージに従って直す |
| 2 | 品質の条件を満たさない | 要約と報告の生き残りを mutation-test-loop で扱う |
| 4 | 変異なしでテストが失敗する | gamut ではなくテストを直す |
| 5 | 変異がテストに届かない（自己検査の失敗） | 対象のコードが `recover` で `panic` を握りつぶしていないかを確かめる |

## CI（GitHub Actions）

```yaml
- uses: actions/checkout@v6
- uses: actions/setup-go@v7
  with:
    go-version-file: go.mod
- uses: ambi/gamut@v0.1.0
  with:
    version: v0.1.0
    args: ./...
```

`version` は Action が入れる gamut の版である。Action の参照と同じ版に固定する。

Pull Request では基準のコミットからの差分だけを判定し、生き残りをその行に注釈として出す。
`.gamut/` は Actions のキャッシュに保存し、品質の条件で失敗しても保存する。
全体を判定するときは `since: none` を渡す。
