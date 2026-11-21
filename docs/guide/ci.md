# gamut の手引き：CI

この文書は、GitHub Actions での実行と、シャードでの分割を説明する。

## GitHub Actions

GitHub Actions では、リポジトリのルートの `action.yml` を使える。
Pull Request では基準のコミットからの差分だけを判定し、生き残りをその行に注釈として出す。
`.gamut/` は Actions のキャッシュに保存し、品質の条件で失敗しても保存する。

```yaml
- uses: actions/checkout@v6
- uses: actions/setup-go@v7
  with:
    go-version-file: go.mod
- uses: ambi/gamut@v0.1.0
  with:
    version: v0.1.0
    args: -threshold-break 70 ./...
```

`version` は Action が `go install` する gamut の版で、既定は `latest` である。Action の参照と同じ版に固定する。

## シャード

大きなモジュールは、CI のジョブごとに `-shard` で分けて判定し、`gamut merge` でまとめられる。
各シャードのキャッシュは、そのシャードの判定を持つ。`gamut merge` はキャッシュだけを読み、報告とまとめたキャッシュを書く。

```console
gamut -shard 1/2 -cache c1.json ./...   # ジョブ 1
gamut -shard 2/2 -cache c2.json ./...   # ジョブ 2
gamut merge -o report.json -cache-out .gamut/cache.json c1.json c2.json
```
