---
status: in_progress
stage: 3
depends_on: [007, 014]
created_at: 2026-11-20
---

# Provide GitHub annotations and an Action

## Motivation

On a pull request, survivors of the diff are easiest to fix when they appear as annotations on their lines.

## Completion criteria

- A unit test checks that the annotations follow GitHub's format.
- Running the Action on a pull request of a public repository puts annotations on the lines.

## Progress

It stays `in_progress` until the check on a pull request of a public repository is done; publishing and opening a pull request are the user's decisions.

- `-annotations github` writes unaccepted survivors as `::error` in critical regions and `::warning` elsewhere, with the class and mutator in the title and the mutation, a hint, and the accept command in the message, escaped by GitHub's rules. Files are relative to `git rev-parse --show-toplevel`. `TestWriteGitHubAnnotations` checks the format and escaping.
- `action.yml` is a composite Action with the inputs `since`, `args`, `working-directory`, `cache`, and `version`. It saves `.gamut/` with `actions/cache` even when the quality gate fails, then exits with gamut's code.
- The run step passes shellcheck. Locally it fetched the base commit from a shallow clone and judged with `-since`, and a full run with `since: none` printed annotations and exit code 2 under `-fail-on-survived`.
- Remaining: run the Action on a pull request of a public repository and check the annotations and the cache save and restore.
