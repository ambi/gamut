---
status: pending
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
