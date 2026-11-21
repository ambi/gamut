---
status: done
stage: 公開
depends_on: [015, 026]
created_at: 2026-11-21
---

# 機械向けの報告を mutation-testing-elements の形式 1 つにし、報告を入力に使わない

## 動機

gamut の機械向けの報告は 2 つある。`-o` の JSON（`files[].mutations[]` の形に gamut のフィールドを足したもの）と、`-reporter json` の mutation-testing-elements の形式である。
前者には公開された仕様も版もなく、読む道具もない。後者は JSON Schema に版があり、閲覧画面やダッシュボードが読める。
2 つを保つと、gamut の情報を足すたびに両方の形を決めることになる。
さらに `gamut merge` が `-o` の報告を入力に読むので、報告の形が内部の都合に縛られている。

## 対象範囲

- `-o` と `-reporter json` は mutation-testing-elements の形式（スキーマ 2）を書く。`-reporter gomutants` をなくす。
- gamut 固有の情報を、スキーマが禁じない追加のフィールドとして載せる。
- `gamut merge` は、シャードのキャッシュに保存した、その実行の報告からまとめる。報告のファイルを読まない。
- canary の検算、端から端までのテスト、README、設計、skill を新しい形に合わせる。

## 対象外

- 標準出力の要約、エージェント向けの報告（`-reporter agent`）、GitHub の注釈の形。どれも内部の報告から作り、今の形のままにする。
- キャッシュの形。内部の状態なので、報告とは独立に変えてよい。

## 設計

### 状態

変異体の `status` はスキーマの値だけを使い、gamut の判定は次のように対応させる。

| gamut の判定 | `status` | `statusReason` |
| --- | --- | --- |
| `KILLED` | `Killed` | |
| `LIVED` | `Survived` | `not infected`、`not detected` など、生き残りの分類 |
| `NOT COVERED` | `NoCoverage` | |
| `NOT VIABLE` | `CompileError` | |
| `TIMED OUT` | `Timeout` | |
| `PENDING` | `Pending` | |
| `EQUIVALENT` | `Ignored` | `equivalent: <理由>` |
| 受理した変異体 | `Ignored` | `accepted: <理由>` |
| 無効化コメントで止めた変異体 | `Ignored` | `ignored by comment: <理由>` |

受理した変異体は判定を保つので、判定した状態を追加のフィールドに残す。

### 追加のフィールド

スキーマが将来フィールドを足しても衝突しないよう、gamut 固有のフィールドは `gamut` という名前のオブジェクトにまとめる。

- 変異体の `gamut`：`original`（置き換える前の字句）、`mode`（`schemata` か `standalone`）、`cached`、`critical`、`previouslyKilledBy`、`acceptedStatus`（受理した変異体の判定した状態）。値がなければ省く。
- 最上位の `gamut`：`version`、`goModule`、`package`、`packages`、`mutationScore`、`thresholdBreak`、`elapsedTime`、`counts`（状態ごとの数と、pending、arid、ignored、equivalent、accepted の数）、`staleAccepted`、`unrepeatableTests`、`maxDepthExcluded`、`skippedPackages`、`killMatrix`。
- スキーマにあるものはスキーマのフィールドを使う。到達したテストは `coveredBy`、検出したテストは `killedBy`（kill matrix ならすべて）、gamut の版は `framework.version`。
- `location` の終わりは、置き換える字句が複数行にわたるとき（`BRANCH_*`、`RETURN_ERROR_NIL`）も正しい行と列にする。

### merge

判定の実行は、終わるたびにその実行の内部の報告をキャッシュに保存する（最後の 1 回分）。
`gamut merge [-o <報告>] [-cache-out <キャッシュ>] <シャードのキャッシュ>...` は、各キャッシュの最後の報告を、今と同じ規則（同じ変異体は `PENDING` でない判定を採る）でまとめる。
報告のファイルは元のソースを埋め込むので、merge はモジュールの中で実行し、ソースを作業ディレクトリのモジュールから読む。

採らない案：

- シャードの mutation-testing-elements の報告を読んでまとめる。報告を入力に使うと、報告の形が再び内部の都合に縛られる。
- gomutants 形式を変換の reporter として残す。読む道具がなく、保つ費用だけが残る。

## 完了の条件

- `-o` の報告が、`internal/report/testdata` の JSON Schema に照らして妥当である（単体テスト）。
- 状態の対応、追加のフィールド、複数行の `location` を、報告の単体テストで表明する。
- `gamut merge` が、シャードのキャッシュだけから、1 回で実行したのと同じ判定の報告を作る（端から端までのテスト）。
- `-reporter gomutants` は誤り（終了コード 1）になる。
- canary の検算が新しい形で通る。

## 検証

- `mise run verify`

## 要約

- `-o` と `-reporter json` は mutation-testing-elements の形式を書き、`-reporter gomutants` はなくした（未知の形式として終了コード 1）。`-o` があれば json はそこへ、なければ `-report-dir` の `mutation.json` へ書く。
- gamut 固有の情報は、変異体と最上位の `gamut` オブジェクトに、内部の型から切り離した camelCase の出力の型（`MTEGamut`、`MTEMutantGamut` など）で写した。内部の `report.Report` はキャッシュのためだけに JSON にする。
- 受理した変異体は `Ignored` にし、判定した状態を `gamut.acceptedStatus` に残した。等価と無効化コメントも `Ignored` で、`statusReason` の接頭辞（`accepted: `、`equivalent: `、`ignored by comment: `）で区別する。
- `location` の終わりを、複数行の字句でも正しくした（039 の `BRANCH_*` と `RETURN_ERROR_NIL` で、終わりが同じ行の先へ伸びていた）。
- `gamut merge` は、判定の実行がキャッシュに残す最後の報告（`cache.ReadLast`）だけからまとめる。引数はシャードのキャッシュにし、`-cache` をなくした。報告は元のソースを埋め込むので、シャードが判定したモジュールのディレクトリ（内部の報告の `module_dir`）があればそこから、なければ作業ディレクトリのモジュールから読む。端から端までのテストの作業ディレクトリは対象のモジュールと違うので、この順にした。
- 端から端までのテストは、報告を読む補助関数（`readMTE`、`readReport`）を 1 つにまとめ、状態の文字列をスキーマの名前にした。canary の検算も新しい形にした。
- 0.1.0 はまだ公開していないので、変更履歴は 0.1.0 の項目を書き換えるだけにした。
