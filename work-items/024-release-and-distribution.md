---
status: done
stage: release
depends_on: [001, 002]
created_at: 2026-11-20
---

# Publish versions installable with `go install`

## Motivation

Users, idmagic included, need published versions to pin.

## Summary

- The module path stays `github.com/ambi/gamut`, the license is MIT, and the first version is 0.1.0 (the user's decisions).
- `LICENSE`, `CHANGELOG.md`, and `.github/workflows/ci.yml` (`mise run verify` on Linux and macOS) were added. Actions in CI and `action.yml` are pinned by commit hash, following zizmor.
- `gamut -version` prints the module version from `debug.ReadBuildInfo`, or `devel` with the commit for local builds.
- The README describes installation, the compatibility note until 1.0.0, and that Windows is not verified. AGENTS.md describes the release steps.
- Before publishing, `go install github.com/ambi/gamut/cmd/gamut@v0.1.0` was checked through a file GOPROXY with empty GOPATH, module cache, and build cache; it printed `gamut v0.1.0` and passed the canary check.
- Remaining for the user: tagging `v0.1.0`, publishing, and checking CI and `go install` of the published version.
