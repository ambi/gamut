---
status: pending
stage: 3
depends_on: []
created_at: 2026-11-20
---

# 設定ファイルを読む

## 動機

変異演算子、arid の規則、重要領域、閾値、タイムアウトを毎回フラグで渡すのは、CI と手元で設定がずれる原因になる。

## 対象範囲

- モジュールのルートの `gamut.json` を読む。`--config <path>` で別のファイルを指定できる。
- 項目：`mutators`、`exclude_files`（正規表現）、`arid`（呼び出しの glob）、`critical`（パスの glob）、`thresholds`、`timeout`、`workers`、`tags`、`test_flags`。
- フラグは設定ファイルより優先する。
- JSON Schema を `schema/gamut.schema.json` に置く。
- 未知の項目は終了コード 1 で拒否する。

## 対象外

- YAML と TOML。依存を増やさないため、標準ライブラリで読める JSON に限る。

## 設計

設定はキャッシュの環境のハッシュ（[004](004-result-cache.md)）に含める。
判定に影響しない項目（`workers`）は除く。

## 完了の条件

- 設定ファイルの各項目が、同じ意味のフラグと同じ判定を生む。
- 未知の項目で終了コード 1 になる。

## 検証

- 設定の読み込みの単体テストと、固定モジュールでの端から端までのテスト。
