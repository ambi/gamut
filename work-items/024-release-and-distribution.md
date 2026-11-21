---
status: done
stage: 公開
depends_on: [001, 002]
created_at: 2026-11-20
---

# 版を公開し、`go install` で入れられるようにする

## 動機

利用者（idmagic を含む）が版を固定して使うには、公開された版が要る。
idmagic の切り替え（idmagic の wi-66093）は、`go install` で入れられる固定の版を条件にしている。

## 対象範囲

- 公開先のリポジトリを決め、モジュールパスをそれに合わせる（今は `github.com/ambi/gamut`）。
- 使用許諾を決めて `LICENSE` を置く。
- `CHANGELOG.md` を置き、識別子の計算と報告の形の変更を必ず記録する。
- CI：`mise run verify` を Linux と macOS で実行する。Windows はプロセスの扱いを確かめてから加える。
- タグで版を切り、`go install github.com/<owner>/gamut/cmd/gamut@vX.Y.Z` で入れられることを確かめる。
- `gamut -version` を足す。版はビルド情報（`debug.ReadBuildInfo`）から読む。

## 対象外

- バイナリの配布（Homebrew、GitHub Releases の成果物）。`go install` で足りるかを使ってから判断する。

## 設計

最初の版は 0.1.0 とし、1.0.0 までは報告の形と識別子の計算を変えうることを README に書く。

## 完了の条件

- 別の環境で `go install` した gamut が、canary の検算を通る。

## 検証

- CI の実行と、`go install` した版での canary の検算。

## 要約

- モジュールパスは `github.com/ambi/gamut` のまま、使用許諾は MIT、最初の版は 0.1.0 にした（利用者の判断）。
- `LICENSE`、`CHANGELOG.md`（利用者の記録に影響する変更の種類と、0.1.0 の内容）、`.github/workflows/ci.yml`（Linux と macOS で `mise run verify`）を置いた。CI と `action.yml` のアクションは、zizmor の指摘に従いコミットのハッシュで固定した。
- `gamut -version` は `debug.ReadBuildInfo` のモジュールの版を出し、手元のビルドで版がなければ `devel` とコミットを出す（`versionOf` の表のテスト）。Go 1.27 は手元のビルドにも擬似版を付ける。
- README に導入（`go install ...@v0.1.0`）、1.0.0 までの互換の注記、Windows が未確認であることを書き、例を `./bin/gamut` から `gamut` に、Action の例を `@v0.1.0` と `version: v0.1.0` に改めた。AGENTS.md にリリースの手順を書き、識別子の変更などの記録先を CHANGELOG.md にした。
- 完了の条件は、公開前なので、作業ツリーから作ったモジュールの zip をファイルの GOPROXY に置き、空の GOPATH、モジュールキャッシュ、ビルドキャッシュで `go install github.com/ambi/gamut/cmd/gamut@v0.1.0` して確かめた。`gamut v0.1.0` を返し、canary の検算を通った。
- 残るのは、タグ `v0.1.0` の作成とリポジトリへの公開（利用者が行う）、公開後の CI の実行と、公開された版での `go install` の確認である。
