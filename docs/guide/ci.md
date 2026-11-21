# gamut guide: CI

This document covers running on GitHub Actions and splitting runs into shards.

## GitHub Actions

On GitHub Actions you can use `action.yml` at the repository root.
On a pull request it judges only the diff from the base commit and annotates the survivors on their lines.
It saves `.gamut/` to the Actions cache, even when the quality gate fails.

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

`version` is the gamut version the Action installs with `go install`, `latest` by default. Pin it to the same version as the Action's ref.

## Shards

A large module can be judged in parts with `-shard`, one per CI job, and combined with `gamut merge`.
Each shard's cache holds that shard's verdicts. `gamut merge` reads only the caches and writes the report and the merged cache.

```console
gamut -shard 1/2 -cache c1.json ./...   # job 1
gamut -shard 2/2 -cache c2.json ./...   # job 2
gamut merge -o report.json -cache-out .gamut/cache.json c1.json c2.json
```
