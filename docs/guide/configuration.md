# gamut の手引き：設定と変異演算子

この文書は、設定ファイル、変異演算子、無効化コメント、等価な変異体の受理と自動検出を説明する。

## 設定ファイル

フラグの既定値は、モジュールのルートの `gamut.json` で変えられる。
コマンドラインのフラグは設定ファイルより優先する。
未知の項目があれば終了コード 1 で止まる。
形式は [schema/gamut.schema.json](../../schema/gamut.schema.json) が定める。

```json
{
  "mutators": ["ARITHMETIC_BASE", "CONDITIONALS_BOUNDARY", "CONDITIONALS_NEGATION"],
  "exclude_files": ["_gen\\.go$", "^internal/legacy/"],
  "timeout": { "factor": 5, "min": "2s" },
  "workers": 4,
  "tags": ["integration"],
  "test_flags": ["-short"],
  "critical": ["internal/auth/**", "billing/fee.go"],
  "thresholds": { "high": 80, "low": 60, "break": 70 },
  "fail_on_survived": false
}
```

## 変異演算子

既定の変異演算子は `ARITHMETIC_BASE`、`CONDITIONALS_BOUNDARY`、`CONDITIONALS_NEGATION`、`INCREMENT_DECREMENT`、`INVERT_NEGATIVES`、`RETURN_ZERO`（戻り値を持つ関数の本体を、ゼロ値を返す文にする。`error` は `nil`）、`STATEMENT_REMOVE`（値を返さない呼び出しの文を実行しない。ログ出力を除く）である。
名前は、Gremlins と gomutants に同じ演算子があればそれに合わせる。
次は既定で無効で、`-only` で選んだときだけ使う。既定の集合の決め方は [設計](../design/mutants.md#既定の集合) が定める。
ログ出力、メトリクス、エラーメッセージ、`String()` の本体など、テストが観測しないことの多いコードは変異しない。規則は [設計](../design/mutants.md#arid-のコード) が定め、設定の `arid` か `-arid-call`、`-arid-package` で足せる。

| 名前 | 変異 |
| --- | --- |
| `INVERT_BITWISE` | `&`↔`\|`、`^`→`&`、`&^`→`&`、`<<`↔`>>` |
| `INVERT_ASSIGNMENTS` | `+=`↔`-=`、`*=`↔`/=`、`%=`→`*=` |
| `INVERT_BITWISE_ASSIGNMENTS` | `&=`↔`\|=`、`^=`→`&=`、`&^=`→`&=`、`<<=`↔`>>=` |
| `INVERT_LOOP_CTRL` | `break`↔`continue` |
| `INVERT_LOGICAL` | `&&`↔`\|\|`（短絡評価を保つ） |
| `REMOVE_LOGICAL_NOT` | `!x`→`x` |
| `BOOLEAN_LITERAL` | `true`↔`false` |
| `CONDITIONS_CONSTANT` | `if` の条件を `true`／`false`、`for` の条件を `false`、`&&` の被演算子を `true`、`\|\|` の被演算子を `false` に |
| `REMOVE_SELF_ASSIGNMENTS` | `x += y`→`x = y`（複合代入すべて） |
| `INTEGER_INCREMENT`、`INTEGER_DECREMENT` | 整数のリテラルを 1 増やす、1 減らす（`make` の容量を除く） |
| `FLOAT_INCREMENT`、`FLOAT_DECREMENT` | 浮動小数点数のリテラルを 1 増やす、1 減らす |
| `BRANCH_IF`、`BRANCH_ELSE`、`BRANCH_CASE` | `if`、`else`、`switch` の `case` の本体を実行しない |
| `ERRORF_WRAP` | `fmt.Errorf` の `%w` を `%v` にする |
| `RETURN_ERROR_NIL` | `return` の `error` 型の結果を `nil` にする |

## 無効化コメント

変異しても観測できない箇所は、理由とともにコメントで止める。

```go
return a * b //gamut:ignore -- the product is only a cache key

//gamut:ignore CONDITIONALS_BOUNDARY -- equal values never reach here
if a < b {

// Fast は Slow と同じ結果を返す近道である。
//
//gamut:ignore -- a fast path with the same result as Slow
func Fast(a, b int) bool {

//gamut:ignore-start ARITHMETIC_BASE -- defensive arithmetic
...
//gamut:ignore-end
```

行末のコメントはその行、単独の行のコメントは次の行、関数の宣言の直前のコメントは関数全体、`ignore-start` と `ignore-end` はその間を止める。
演算子を省けばすべての演算子を止める。
理由のないコメントは警告する。
規則は [設計](../design/mutants.md#無効化コメント) が定める。

## 等価な変異体

どの入力でも観測できない変異体（等価な変異体）は、ソースを変えずに、モジュールのルートの `gamut.accepted.json` に受理として記録できる。

```console
# 報告の id を渡して受理する。
gamut accept -reason 'equivalent: callers read only the sign' 'pkg/file.go::ARITHMETIC_BASE::Double::1a2b3c4d5e'
# テストが検出するようになった受理と、変異体がなくなった受理を、判定してから消す。
gamut accept -prune ./path/to/module/...
```

受理した変異体は引き続き判定するが、報告では `Ignored`（`statusReason` は `accepted: <理由>`）にして数と点数に含めず、要約の直す一覧にも並べない。
要らなくなった受理は、要約と報告の `gamut.staleAccepted` に並ぶ。
規則は [設計](../design/mutants.md#等価な変異体の受理) が定める。

`-detect-equivalent` は、生き残りごとに変異を当てたパッケージをコンパイルし、機械語が元と同じもの（`x * 1` を `x / 1` にする変異体など）を `EQUIVALENT` にする。
`go tool compile` を直接呼ぶので、共有のビルドキャッシュは増えない。
規則は [設計](../design/mutants.md#等価な変異体の自動検出) が定める。
