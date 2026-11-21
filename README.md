# gamut

gamut は Go 向けのミューテーションテストツールである。
パッケージのソースに機械的な変異（`<` を `<=` にするなど）を加え、テストがその変異を検出できるかを調べる。

- **速い**：全変異体を 1 回のビルドに埋め込み（mutant schemata）、どれを有効にするかを実行時に選ぶ。2 回目からは、変わった箇所の変異体だけを判定する。
- **ビルドキャッシュを汚さない**：変異体ごとのビルド結果を共有の Go ビルドキャッシュへ書かない。
- **生き残りを仕分ける**：テストが到達しなかった（`NOT COVERED`）、到達したが値が変わらなかった（`not infected`）、値は変わったが失敗しなかった（`not detected`）に分ける。直し方がそれぞれ違うためである。
- **依存がない**：Go の標準ライブラリと `go` コマンドだけを使う。

## 導入

Go 1.27 以降が要る。macOS と Linux で動作を確かめている。Windows はまだ確かめていない。

```console
go install github.com/ambi/gamut/cmd/gamut@v0.1.0
```

1.0.0 までは、マイナー版で報告の形と変異体の識別子の計算を変えうる。変更は [CHANGELOG.md](CHANGELOG.md) に記録する。

## 使い方

```console
# モジュール全体を判定し、生き残りを理由ごとに並べる。
gamut ./...
# main から変えた箇所の変異体だけを判定する。
gamut -since main ./...
# 生き残りを埋めるテストを書いたら、その変異体だけを確かめ直す。
gamut -mutant '<報告の id>' ./path/to/pkg
# mutation-testing-elements の JSON と、それを表示する HTML を書く。
gamut -o report.json -reporter text -reporter html ./...
```

判定と被覆の記録は `.gamut/` に保存し、次の実行では変わった箇所だけをやり直す。`.gamut/` はリポジトリに入れない。

## 文書

| 文書 | 内容 |
| --- | --- |
| [手引き：実行](docs/guide/running.md) | 対象の指定、フラグの一覧、テストプロセス、重要領域と品質の条件、終了コード |
| [手引き：設定と変異演算子](docs/guide/configuration.md) | 設定ファイル `gamut.json`、変異演算子、無効化コメント、等価な変異体の受理と自動検出 |
| [手引き：繰り返しの実行](docs/guide/incremental.md) | 判定の再利用、`-since`、`-mutant`、統合モード、`gamut select-tests` |
| [手引き：報告](docs/guide/reports.md) | JSON と HTML の報告、エージェント向けの報告、不要かもしれないコードの報告 |
| [手引き：CI](docs/guide/ci.md) | GitHub Actions、シャードと `gamut merge` |
| [設計](docs/design/README.md) | gamut の動き方と、その判断の理由 |
| [機能の比較](docs/evaluation/comparison.md) | 主要なミューテーションテストツールとの機能の比較 |
| [測定](docs/evaluation/benchmarks.md) | 実在のパッケージでの実行時間とビルドキャッシュの増加 |

コーディングエージェント向けの skill を [skills/](skills/) に置く。導入と CI は `gamut`、生き残りを 1 体ずつ埋める手順は `mutation-test-loop` が定める。

## 開発

コマンドは `mise run <task>` で実行する（[AGENTS.md](AGENTS.md)）。

```console
mise run verify
```

## 使用許諾

[MIT](LICENSE)
