---
status: in_progress
stage: 3
depends_on: [007, 014]
created_at: 2026-11-20
---

# GitHub の注釈と Action を用意する

## 動機

Pull Request では、差分の生き残りがその行に注釈として出ると直しやすい。

## 対象範囲

- `--annotations github` で、報告対象の生き残りを `::warning file=,line=,col=,title=` で出す。題名に分類を、本文に変異と直し方の指針を入れる。受理の命令も添える。
- リポジトリのルートに `action.yml` を置く。入力は `since`（既定は Pull Request の基準のコミット）、`args`、`working-directory`、`cache`、`version`。
- Action は `.gamut/` を Actions のキャッシュに保存し、Pull Request では基準のブランチのキャッシュを読む。
- 浅いチェックアウトでは基準のコミットを取得する。

## 対象外

- GitHub 以外の CI。

## 設計

キャッシュの鍵はモジュール相対のパスを使うので、チェックアウト先のディレクトリが変わっても再利用できる（[004](004-result-cache.md)）。
品質の条件で失敗しても、キャッシュは保存する。判定は有効だからである。

## 完了の条件

- 注釈の出力が GitHub の書式に従うことを単体テストで確かめる。
- 公開リポジトリの Pull Request で Action を動かし、注釈が付くことを確かめる。

## 検証

- 単体テストと、公開リポジトリでの実地の確認。

## 経過

公開リポジトリの Pull Request での確認が残るので、`in_progress` のままにしている。
gamut にはまだリモートのリポジトリがなく、公開と Pull Request の作成は利用者の判断を要する。

- `-annotations github` は、受理していない生き残りを、重要領域なら `::error`、ほかは `::warning` で書く。題名に分類と演算子、本文に変異、直し方の指針、受理の命令を入れ、本文と属性を GitHub の規則でエスケープする。ファイルは `git rev-parse --show-toplevel` からのリポジトリ相対にする。単体テスト（`TestWriteGitHubAnnotations`）で書式とエスケープを確かめた。
- `action.yml` は複合 Action で、`since`、`args`、`working-directory`、`cache`、`version` を受け取る。Actions のキャッシュは `actions/cache/restore@v6` と `actions/cache/save@v6` で、品質の条件で失敗しても保存してから gamut の終了コードで終える。
- 実行の段のスクリプトは shellcheck を通し、手元で、浅いクローンから基準のコミットを取得して `-since` で判定すること、`since: none` の全体の実行で注釈が出て `-fail-on-survived` の終了コード 2 を出力に書くことを確かめた。
- 残り：公開リポジトリの Pull Request で Action を動かし、注釈がその行に付くこと、キャッシュの保存と復元を確かめる。`go install github.com/ambi/gamut/cmd/gamut@latest` が動くには、024 の公開が要る。
