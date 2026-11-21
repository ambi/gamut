---
status: done
stage: 3
depends_on: []
created_at: 2026-11-20
---

# mutation-testing-elements の JSON と HTML を出す

## 動機

mutation-testing-elements のスキーマ（`schemaVersion: "2"`）は、ミューテーションテストの報告で広く使われる形式である。
このスキーマの JSON を出せば、それを読む既存の HTML の閲覧画面やダッシュボードをそのまま使える。
gamut は gomutants 互換の JSON と標準出力の要約しか出さない。

## 対象範囲

- `--reporter json|html|text|gomutants`（繰り返し可）を足す。既定は `text` と、`-o` を指定したときの `gomutants`。
- `json` は mutation-testing-elements のスキーマ 2 で、ファイルの内容、変異体の位置、状態、検出テスト、到達したテストを含む。
- `html` は JSON を埋め込んだ 1 ファイルで、閲覧画面のスクリプトを固定した版で読み込む。
- 状態の対応：`KILLED`→Killed、`LIVED`→Survived、`NOT COVERED`→NoCoverage、`NOT VIABLE`→CompileError、`TIMED OUT`→Timeout、`IGNORED`→Ignored、`PENDING`→Pending。生き残りの理由は `statusReason` に入れる。
- 出力先は `--report-dir`（既定は `.gamut/report`）。

## 対象外

- 閲覧画面のスクリプトの同梱。同梱すると版の更新と使用許諾の管理が要る。

## 設計

スキーマは公開されている JSON Schema に対して検証するテストを置く。
検証にはスキーマのファイルを `testdata` に固定して使い、ネットワークに依存しない。

## 完了の条件

- 固定モジュールの JSON がスキーマの検証を通る。
- HTML をブラウザーで開くと、ファイルごとの変異体が表示される（手で確かめる）。

## 検証

- スキーマの検証の単体テスト。

## 要約

- `-reporter text|gomutants|json|html`（繰り返し可）と `-report-dir`（既定 `.gamut/report`）を足した。既定は `text` で、`-o` を指定すれば `gomutants` も書く。変更のない再実行で前回の報告を返すときも、同じ形式で書く。
- `json` は mutation-testing-elements のスキーマ 2 で、ファイルの中身、位置（字句の長さの範囲）、状態、`statusReason`、`killedBy`、`coveredBy`、`testFiles` を持つ。中身はモジュールのディレクトリから読む。
- 状態の `IGNORED` は gamut にないので、数えない変異体（無効化コメント、受理、`EQUIVALENT`）を Ignored にし、理由を `statusReason` に入れた。閾値はスキーマが整数を求めるので丸める。
- `html` は報告を埋め込んだ 1 ファイルで、閲覧画面のスクリプトを mutation-testing-elements 3.9.0 に固定して CDN から読み込む。
- スキーマ（mutation-testing-report-schema 3.9.0）と使用許諾（Apache License 2.0）を `internal/report/testdata` に固定し、このスキーマが使うキーワードだけを扱うテストの中の検証器で、全状態を含む報告の適合を確かめた。検証器そのものが壊れた報告を拒むことも確かめた。
- 端から端までのテストで、`json` の状態が gomutants の形式の状態と対応すること、`html` が報告を埋め込むこと、未知の形式が終了コード 1 になることを表明した。
- idmagic の `backend/oauth2/internal/token/usecases` の HTML を Chrome のヘッドレスで開き、ファイルごとの変異体の数が表示されること、閲覧画面の点数（79.35%）が gamut の `mutation_score` と一致することを確かめた。
