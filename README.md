# gamut

gamut は Go 向けのミューテーションテストツールである。
パッケージのソースに機械的な変異（`<` を `<=` にするなど）を加え、テストがその変異を検出できるかを調べる。

- **速い**：全変異体を 1 回のビルドに埋め込み（mutant schemata）、どれを有効にするかを実行時に選ぶ。変異体ごとにコンパイルしない。
- **ビルドキャッシュを汚さない**：変異体ごとのビルド結果を共有の Go ビルドキャッシュへ書かない。
- **生き残りを仕分ける**：テストが到達しなかった（`NOT COVERED`）、到達したが値が変わらなかった（`LIVED`、`not infected`）、値は変わったが失敗しなかった（`LIVED`、`not detected`）に分ける。直し方がそれぞれ違うためである。
- **依存がない**：Go の標準ライブラリと `go` コマンドだけを使う。

設計は [docs/design.md](docs/design.md) が定める。

## 使い方

```console
go build -o bin/gamut ./cmd/gamut
./bin/gamut -o report.json ./path/to/package
./bin/gamut -o report.json ./path/to/module/...
```

引数はパッケージディレクトリか、その下のすべてのパッケージを表す `<ディレクトリ>/...` で、複数を並べられる。
複数のパッケージを 1 回で判定し、報告を 1 つにまとめる。
対象のパッケージは同じモジュールに属していなければならない。
テストファイルを持たないパッケージの変異体は、ビルドせずに `NOT COVERED` とする。
あるパッケージの変異は、そのパッケージのテストだけで判定する。別の対象パッケージのテストは、変異のない版にリンクされる。

| フラグ | 既定値 | 意味 |
| --- | --- | --- |
| `-workers` | CPU の数の半分（1 以上） | 並列に動かすテストプロセスの数 |
| `-only` | すべて | 有効にする変異演算子（カンマ区切り） |
| `-exclude-files` | なし | モジュール相対のパスがこの正規表現に当たるファイルを変異しない。繰り返し指定できる |
| `-config` | モジュールのルートの `gamut.json` | 設定ファイルのパス |
| `-o` | なし | gomutants 互換の JSON の報告の書き出し先 |
| `-reporter` | `text`（`-o` があれば `gomutants` も） | 報告の形式。`text`（標準出力の要約）、`gomutants`、`json`（mutation-testing-elements）、`html`、`agent`（エージェント向けの JSON を標準出力に）。繰り返し指定できる |
| `-report-dir` | `.gamut/report` | `json`、`html`、`-o` のない `gomutants` の書き出し先 |
| `-annotations` | なし | `github` なら、直すべき生き残りを GitHub Actions の注釈として標準出力に書く |
| `-dry-run` | false | 変異体と構築可能性だけを一覧にする |
| `-timeout-factor` | 5 | 選んだテストの基準の時間に掛ける係数 |
| `-timeout-min` | 2s | 時間切れの上限に足す時間 |
| `-q` | false | 進行状況を出さない |
| `-tags` | なし | 読み込み、ビルド、型検査に渡すビルドタグ（カンマ区切り） |
| `-test-flags` | なし | テストバイナリのすべての実行に渡すフラグ（空白区切り、例：`"-short"`） |
| `-cache` | `.gamut/cache.json` | 判定と被覆の記録のキャッシュの読み書き先（作業ディレクトリからの相対） |
| `-no-cache` | false | キャッシュを読みも書きもしない |
| `-since` | なし | この Git の参照と `HEAD` の merge-base から作業ツリーまでの差分に交わる変異体だけを判定し、残りを `PENDING` にする |
| `-scope` | `node` | `-since` で差分と交わりを取る区間。`node` は変異箇所の式、`decl` は囲む宣言 |
| `-include-test-changes` | false | `-since` で、差分が変えたテストが到達する、差分の外の生き残りも再検査する |
| `-mutant` | なし | この識別子（一意な前方一致でもよい）の変異体だけを、キャッシュを使わずに判定する。繰り返し指定できる |
| `-integration` | false | 対象パッケージを import するモジュール内のパッケージのテストでも検出を試す |
| `-isolate` | false | テストプロセスを変異体の間で使い回さず、変異体ごとに起動する |
| `-detect-equivalent` | false | 生き残りごとにパッケージをコンパイルし、機械語が元と同じなら `EQUIVALENT` にする（ビルドキャッシュには書かない） |
| `-critical` | なし | モジュール相対のパスがこの glob（`**` は任意の深さ）に当たるファイルを重要領域にする。繰り返し指定できる |
| `-threshold-high` | 80 | 点数がこれ以上なら要約に `[high]` を付ける |
| `-threshold-low` | 60 | 点数がこれ以上なら `[low]`、未満なら `[danger]` を付ける |
| `-threshold-break` | 0（無効） | 点数がこれ未満なら終了コード 2 で終える |
| `-fail-on-survived` | false | 受理していない生き残りが 1 体でもあれば終了コード 2 で終える |
| `-cpuprofile` | なし | gamut 自身の CPU プロファイルの書き出し先（gamut の開発用） |

フラグの既定値は、モジュールのルートの `gamut.json` で変えられる。
コマンドラインのフラグは設定ファイルより優先する。
未知の項目があれば終了コード 1 で止まる。
形式は [schema/gamut.schema.json](schema/gamut.schema.json) が定める。

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

変異演算子は `ARITHMETIC_BASE`、`CONDITIONALS_BOUNDARY`、`CONDITIONALS_NEGATION`、`INCREMENT_DECREMENT`、`INVERT_NEGATIVES` で、Gremlins と gomutants と同じ名前である。
テストプロセスは、gamut の 1 回の実行の間、変異体の間で使い回す。
変異体がプロセスの状態を汚したと分かれば、そのプロセスを捨て、新しいプロセスで判定し直す。
判定の前に、変異がテストに届くことを 1 件のテストで確かめる（forced fail）。
届いていなければ判定せずに終了コード 5 で止まる。
変異を無効にした状態で失敗するテストがあるときは、そのテストの出力を見せて終了コード 4 で止まる。

認証や金額の計算のような箇所は、重要領域にして、すべての変異体の検出か受理を求められる。
`-critical` の glob に当たるファイルと、関数の宣言の直前に `//gamut:critical` を書いた関数が重要領域である。
重要領域に受理していない生き残りがあるか、点数（`mutation_score`：検出した変異体の、検出したものと生き残りの和に対する割合）が `-threshold-break` 未満なら、報告と要約を書いたうえで終了コード 2 で終える。

| 終了コード | 意味 |
| --- | --- |
| 0 | 判定を終え、品質の条件を満たした |
| 1 | 引数、設定、読み込み、ビルドなどの誤り |
| 2 | 判定を終えたが、品質の条件を満たさない |
| 4 | 変異なしでテストが失敗し、判定しなかった |
| 5 | forced fail の自己検査で変異がテストに届かないと分かり、判定しなかった |

判定はキャッシュに保存し、次の実行では、囲む宣言、それが参照する同じパッケージの宣言、手元のモジュールの依存パッケージ、到達したテスト、環境（Go の版、gamut、`go.mod`、`go.sum`、フラグ）が変わらない変異体を実行しない。
被覆の記録もテストごとに保存し、変わった宣言に入っていたテスト、変わったテスト、新しいテストだけを測り直す。
コメントや空白の変更、行の移動では実行し直さない。
Ctrl-C で止めても、それまでの判定は保存される。
規則は [設計](docs/design.md#判定の再利用) が定める。

```console
# main から変えた箇所の変異体だけを判定する。ほかは PENDING になる。
./bin/gamut -since main ./path/to/module/...
# 生き残りを埋めるテストを書いたら、その変異体だけを確かめ直す。
./bin/gamut -mutant 'pkg/file.go::CONDITIONALS_BOUNDARY::IsAdult::1a2b' ./path/to/pkg
# 呼び出し元のパッケージのテストでも検出を試す。
./bin/gamut -integration ./path/to/pkg
```

`-since` は、差分の外でも、前回の検出テストが消えた変異体を再検査し、検出されなくなったものを `previously_killed_by` 付きで報告する。
`-integration` では、逆依存パッケージのテストの名前を `<import パス>.<テスト名>` で表す。
どのテストも変異箇所に到達しない逆依存パッケージは、次の実行からビルドしない。
規則は [設計](docs/design.md#判定する変異体の選択) と [設計](docs/design.md#統合モード) が定める。

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
規則は [設計](docs/design.md#無効化コメント) が定める。

どの入力でも観測できない変異体（等価な変異体）は、ソースを変えずに、モジュールのルートの `gamut.accepted.json` に受理として記録できる。

```console
# 報告の id を渡して受理する。
./bin/gamut accept -reason 'equivalent: callers read only the sign' 'pkg/file.go::ARITHMETIC_BASE::Double::1a2b3c4d5e'
# テストが検出するようになった受理と、変異体がなくなった受理を、判定してから消す。
./bin/gamut accept -prune ./path/to/module/...
```

受理した変異体は引き続き判定し、報告に `accepted`（理由）を付けるが、数と効率には含めず、要約の直す一覧にも並べない。
要らなくなった受理は、要約と報告の `stale_accepted` に並ぶ。
規則は [設計](docs/design.md#等価な変異体の受理) が定める。

`-detect-equivalent` は、生き残りごとに変異を当てたパッケージをコンパイルし、機械語が元と同じもの（`x * 1` を `x / 1` にする変異体など）を `EQUIVALENT` にする。
`go tool compile` を直接呼ぶので、共有のビルドキャッシュは増えない。
規則は [設計](docs/design.md#等価な変異体の自動検出) が定める。

JSON の報告は gomutants と同じ `files[].mutations[]` の形に、`reason`、`mode`、`tests`、`killed_by`、`cached`、`previously_killed_by` を足したものである。
`-since` の外の変異体は `PENDING` で、最上位の `mutants_pending` に数え、`mutants_total` と効率には含めない。
コメントで止めた変異体は `files[].mutations[]` に入れず、最上位の `ignored` に理由とともに並べ、`mutants_ignored` に数える。
受理した変異体は `files[].mutations[]` に本当の状態のまま `accepted` を付け、`mutants_accepted` に数える。
`EQUIVALENT` の変異体は `mutants_equivalent` に数え、`mutants_total` と効率には含めない。
`files[].path` はモジュール相対のパスである。
最上位の `package` は、1 パッケージならその import パス、複数なら引数に指定したパターンで、`packages` に判定したパッケージの import パスを並べる。
変異体ごとの `critical`（重要領域か）と、最上位の `mutation_score` と `thresholds` も足す。

`-reporter json` と `-reporter html` は、mutation-testing-elements の形式の `mutation.json` と、それを閲覧画面で表示する `mutation.html` を `-report-dir` に書く。
HTML は閲覧画面のスクリプトを、固定した版で CDN から読み込む。

```console
./bin/gamut -reporter text -reporter html ./path/to/module/...
open .gamut/report/mutation.html
```

コーディングエージェントに生き残りを埋めさせるときは、`-reporter agent` で、生き残りごとの分類、変異の前後の行、囲む宣言のソース、到達したテスト、直し方の指針を JSON で渡す。
手順は [skills/mutation-test-loop](skills/mutation-test-loop/SKILL.md) が、導入と CI は [skills/gamut](skills/gamut/SKILL.md) が定める。

```console
./bin/gamut -reporter agent -since main ./path/to/pkg
```

GitHub Actions では、リポジトリのルートの `action.yml` を使える。
Pull Request では基準のコミットからの差分だけを判定し、生き残りをその行に注釈として出す。
`.gamut/` は Actions のキャッシュに保存し、品質の条件で失敗しても保存する。

```yaml
- uses: actions/checkout@v6
- uses: actions/setup-go@v7
  with:
    go-version-file: go.mod
- uses: ambi/gamut@main
  with:
    args: -threshold-break 70 ./...
```

## 測定

2026-11-21、Apple Silicon（8 コア）の macOS 27.2、Go 1.27.4。
対象は idmagic の `backend/oauth2/internal/token/usecases`（変異体 562〜563 体、テスト 135 件）で、同じ 5 種類の変異演算子で実行した。
macOS は、新しい実行ファイルを初めて起動するときに検査を行い、このテストバイナリでは約 0.35 秒かかる（[設計](docs/design.md#macos-の初回起動の検査)）。
ソースを変えた後の時間はこの検査を含む。素の `go test` も同じ検査を受ける。

### 初回

| 項目 | gomutants 0.7.3（`--cache off`、ワーカー数 2、2026-11-20） | gamut（`-no-cache`、ワーカー数 2） | gamut（`-no-cache`、既定のワーカー数 4） |
| --- | --- | --- | --- |
| 実行時間 | 352 秒 | 7.6 秒 | 4.7〜5.1 秒 |
| 判定 | killed 439、lived 33、not covered 84、not viable 7 | killed 438、lived 29、not covered 85、not viable 10 | 同左 |

共有ビルドキャッシュの増加は、gomutants が 6,197 ファイル、2.6 GiB だった。
gamut はパッケージごとに書き換えたテストバイナリを 1 回だけビルドするので、ソースを変えるたびに約 6 MiB 増え、同じソースの再実行では増えない（下の「再実行」）。

ワーカー数 4 の gamut の内訳は、構築可能性の判定 0.46 秒、ビルド 0.42 秒、被覆の記録 0.31 秒、変異体の実行 3.2 秒である。
563 体のうち 550 体を schemata に埋め込み、2 体（定数宣言）を個別にビルドした。

両者が生成した変異体の判定の食い違いは 12 件で、すべて原因を確かめた。

| 件数 | 原因 | 正しい判定 |
| --- | --- | --- |
| 6 | `a != "" && a != X` の右辺など、短絡評価で評価されない式。gomutants は行単位の被覆で `LIVED`、gamut は式単位で `NOT COVERED` | gamut |
| 3 | 文字列の `+` を `-` にする変異。gomutants は被覆を先に見て `NOT COVERED`、gamut は型検査を先にして `NOT VIABLE` | どちらも妥当 |
| 2 | 定数宣言（`30 * time.Second`）の変異。gomutants は行が実行されないので `NOT COVERED`、gamut は個別ビルドで全テストを実行して `LIVED` | gamut（定数の値はテストから到達しうる） |
| 1 | gomutants が二項の `-` にも `INVERT_NEGATIVES` を付けている | gamut（単項の `-` だけが対象） |

### 再実行

既定のワーカー数 4 で、キャッシュありの初回の後に実行した。
1 行の編集は、補助関数 `evaluateForServer` に `if` 文を 1 つ足すもので、毎回違う中身にした。
同じ中身の編集を繰り返すと、コンパイルとリンクがビルドキャッシュに当たり、実際より 0.3〜0.5 秒短く出る。

| 実行 | 時間 | 被覆を測ったテスト | 実行した変異体 | 共有ビルドキャッシュの増加 |
| --- | --- | --- | --- | --- |
| キャッシュあり、初回 | 5.1 秒 | 135 | 463 | |
| 変更なし | 0.02 秒 | 0 | 0 | なし |
| 1 行の編集の後 | 1.6〜1.7 秒 | 28（その関数の変異箇所に到達していたテスト） | 34（`evaluateForServer` の 24 体と、それを呼ぶ `EvaluateResourceAccess` の 10 体） | 1 回あたり約 6 MiB |
| 1 行の編集の後（`-since HEAD`） | 1.5 秒 | 28 | 1 | 1 回あたり約 6 MiB |
| 参考：1 行の編集の後の素の `go test` | 0.65 秒（3 回のうち最初の 1 回は 1.3 秒） | | | 1 回あたり約 1.8 MiB |

キャッシュありの初回と、1 行の編集の後の判定は、どちらもキャッシュなしの実行と一致した。
キャッシュファイルは 1.5 MiB だった。
`-since` の 1.5 秒の内訳は、読み込み、差分、鍵、構築可能性で約 0.3 秒、ビルド約 0.66 秒、macOS の初回起動の検査約 0.35 秒、テストの初期化と実行約 0.15 秒である。

`backend/oauth2/internal/token/` の `domain`、`db_memory`、`usecases`（3 パッケージ、593 体、テスト 142 件）では、キャッシュなし 5.7 秒、キャッシュありの初回 5.3 秒、変更なし 0.02 秒、同じ 1 行の編集の後 1.75 秒（被覆を測ったテスト 28 件、実行した変異体 34 体）だった。

### 統合モード

同じ環境で、idmagic の `backend/signinpolicy/domain`（27 体）を、モジュール内の逆依存パッケージ 110 個（テスト 2,773 件）のテストとともに実行した。

| 項目 | gomutants 0.7.3（`--integration --cache=off`、ワーカー数 2） | gamut（`-integration -no-cache`、ワーカー数 2） | gamut（`-integration -no-cache`、既定のワーカー数 4） |
| --- | --- | --- | --- |
| 実行時間 | 748 秒 | 163 秒 | 130 秒（被覆の記録 82 秒） |
| 判定 | killed 29 | killed 27 | 同左 |

統合モードなしの gamut は killed 17、not covered 10 で、統合モードで 10 体が逆依存パッケージのテストに検出された。
両者が生成した 27 体の判定はすべて一致した。
食い違いは gomutants だけが生成した 2 体で、どちらも二項の `-` への `INVERT_NEGATIVES` である（上の表の最後の行と同じ原因）。
110 個の逆依存パッケージのうち 101 個は、どのテストも変異箇所に到達しなかった。

キャッシュありでは、変更のない再実行が 0.04 秒、`domain` の関数に `if` 文を 1 つ足した後が 12 秒（被覆を測ったテスト 75 件、実行した変異体 5 体）だった。

共有ビルドキャッシュの増加は、gomutants の初回が 2,432 ファイル、244 MiB だった。
gamut は、ビルドのフラグを変えた後の初回で 1,432 ファイル、313 MiB、`domain` を 1 回編集するごとに 48〜56 MiB だった。
対象パッケージを変えると、それを import するパッケージ（逆依存パッケージのテストバイナリが含むもの）をすべてコンパイルし直すためで、素の `go test ./...` でも同じものをコンパイルする。
ファイルの数と大きさは、作成時刻が実行の開始より後のものを数えた（更新時刻は、Go が使った項目の時刻を更新するので数えすぎる）。

## 開発

コマンドは `mise run <task>` で実行する（[AGENTS.md](AGENTS.md)）。

```console
mise run verify
```
