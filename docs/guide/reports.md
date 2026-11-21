# gamut の手引き：報告

この文書は、JSON と HTML の報告、エージェント向けの報告、不要かもしれないコードの報告を説明する。

## JSON の報告

JSON の報告（`-o`、`-reporter json`）は [mutation-testing-elements](https://github.com/stryker-mutator/mutation-testing-elements) のスキーマ 2 の形式で、HTML の閲覧画面やダッシュボードがそのまま読める。
`files` の鍵はモジュール相対のパスで、各ファイルは元のソースを埋め込む。
変異体の `status` と、gamut の判定の対応は次のとおりである。

| `status` | 判定 | `statusReason` |
| --- | --- | --- |
| `Killed` | 検出された | |
| `Survived` | 生き残った | `not infected`（到達したが値が変わらなかった）か `not detected`（値は変わったが失敗しなかった） |
| `NoCoverage` | どのテストも到達しなかった | |
| `CompileError` | 構築できない | |
| `Timeout` | 時間切れ（検出に数える） | |
| `Pending` | 選択（`-since`、`-shard`、`-time-budget`）の外で判定しなかった | |
| `Ignored` | 数えない | `accepted: <理由>`（受理）、`equivalent: <理由>`（等価）、`ignored by comment: <理由>`（無効化コメント） |

`killedBy` は検出したテスト（`-kill-matrix` ならすべて）、`coveredBy` は変異箇所に到達したテストである。
gamut 固有の情報は `gamut` という名前のオブジェクトにまとめる。
変異体の `gamut` は、`original`（置き換える前の字句）、`mode`（`schemata` か `standalone`）、`cached`、`critical`（重要領域か）、`previouslyKilledBy`（前回の検出テストが消えた生き残り）、`acceptedStatus`（受理した変異体の判定）を持つ。
最上位の `gamut` は、`goModule`、`package`、`packages`、`mutationScore`、`counts`（状態ごとの数）、`staleAccepted`（要らなくなった受理）、`killMatrix` などを持つ。
`Pending` と `Ignored` は、`counts.total` と点数に含めない。

## HTML の報告

`-reporter html` は、報告を閲覧画面で表示する `mutation.html` を `-report-dir` に書く。
`-o` がなければ、`-reporter json` は `mutation.json` を `-report-dir` に書く。
HTML は閲覧画面のスクリプトを、固定した版で CDN から読み込む。

```console
gamut -reporter text -reporter html ./path/to/module/...
open .gamut/report/mutation.html
```

## エージェント向けの報告

コーディングエージェントに生き残りを埋めさせるときは、`-reporter agent` で、生き残りごとの分類、変異の前後の行、囲む宣言のソース、到達したテスト、直し方の指針を JSON で渡す。
手順は [skills/mutation-test-loop](../../skills/mutation-test-loop/SKILL.md) が、導入と CI は [skills/gamut](../../skills/gamut/SKILL.md) が定める。

```console
gamut -reporter agent -since main ./path/to/pkg
```

## 不要かもしれないコードの報告

`-reporter simplify` は、テストが実行するのに、消しても失敗するテストがないコード（文の削除と条件の定数化の生き残り）を、関数ごとに並べる。
消すか、それを必要とするテストを足すかの見直しの候補である。

```console
gamut -only STATEMENT_REMOVE,CONDITIONS_CONSTANT -reporter simplify ./path/to/pkg
```
