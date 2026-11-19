---
status: pending
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
