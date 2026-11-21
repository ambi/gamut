---
status: done
stage: 3
depends_on: [010]
created_at: 2026-11-20
---

# arid の規則を広げ、設定で足せるようにする

## 動機

Google の研究では、変異を 1 行 1 体にし、観測されないコード（arid）を除くと、1 つの変更あたりの変異体が中央値 820 から 7 に減り、役に立つ報告の割合が 80% から 89% に上がった。
gamut は今、`log` と `log/slog` の呼び出しの引数だけを除く。

## 対象範囲

- 既定の規則：
  - `log`、`log/slog` に加え、`*.Debug*`、`*.Info*`、`*.Warn*`、`*.Error*` のメソッド（ロガーの型に限る）。
  - `fmt.Fprint*` の書き込み先が `os.Stderr` のもの。
  - OpenTelemetry とメトリクスの呼び出し（`go.opentelemetry.io/otel/...`、`expvar`）。
  - `String()`、`GoString()`、`Error()` のメソッドの本体。
  - `time.Now()` と `time.Since()` の結果の算術。
  - `panic` と `errors.New`、`fmt.Errorf` のメッセージの算術。
- ログを出すだけの `if` と文のまとまりは、中身がすべて arid なら全体を arid とする（Google の規則 `arid(n) = simple(n) ? expert(n) : children.every(arid)`）。
- 設定の `arid.calls`（呼び出しの glob）と `arid.packages` で足せる。`--no-arid` で無効にできる。
- 除いた変異体は数を要約に出す。

## 対象外

- 1 行 1 体に絞る機能。測ってから別に判断する。

## 設計

規則は呼び出し先の `types.Object` のパッケージと名前で判定し、字句の一致には頼らない。

## 完了の条件

- 各規則に、除かれる例と除かれない例の固定データがある。
- idmagic の代表パッケージで、規則の追加前後の変異体の数と生き残りの数を測る。

## 検証

- 単体テストと測定。

## 要約

- 規則を `internal/mutant/arid.go` にまとめ、どれも呼び出し先の `types.Object` のパッケージと名前で判定する。出力だけの呼び出し（ログ、メトリクス、トレース、`os.Stderr` への書き出し、設定で足したもの）は、引数を変異せず文の削除もしない。メッセージ（`panic`、`errors.New`、`fmt.Errorf`）は引数だけを除き、`panic` の文の削除は残す。
- 「ロガーの型」は、受け手の型の名前が `Log` を含むか、既知のロガーのパッケージのものとした。メソッドの名前だけで判定すると、`Info` という名前の別の型のメソッドまで除いてしまう。
- まとまりの規則は、中身がすべて出力だけの呼び出しの文である `if` とブロックに当て、条件を含めて除く。
- 設定の `arid.calls` と `arid.packages` に、同じ意味のフラグ `-arid-call` と `-arid-package` を足した（010 の、設定の各項目に同じ意味のフラグを持たせる規則に合わせた）。`-no-arid` ですべて無効にする。
- 除いた変異体を数えるため、列挙を `mutant.EnumerateWith` にし、arid の中の変異体は作らずに数える。数は報告の `mutants_arid` と要約に出す。
- 表の単体テスト（`TestArid_Rules`）で各規則の除く例と除かない例を、端から端までのテスト（`TestArid_FlagsAndCount`）でフラグと数を表明した。
- idmagic の 4 パッケージで規則の前後を測った（設計の「arid のコード」）。`support_http` で生き残りが 63 体から 56 体に減り、除いた生き残りはどれもテストが観測しないログだった。
