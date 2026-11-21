---
status: pending
stage: release
depends_on: [002, 030]
created_at: 2026-11-21
---

# Verify that verdicts on Windows match macOS and Linux

## Motivation

On Unix, stopping a test binary sends `SIGKILL` to its process group, but on Windows only the test binary stops, so child processes may remain and hold the output pipe. The judging test returns results on file descriptor 3, and `ExtraFiles` on Windows is unverified. Nothing has been run on Windows.

## Scope

- Pass the end-to-end tests and the canary check in Windows CI.
- Stop child processes too on timeouts and cancellation (Job Object).
- Verify that the judging test's result channel works on Windows, or switch to another channel (such as a named pipe).
- Check path separators (identifiers and report paths use `/`) and line endings (positions and diff tokens in CRLF sources).

## Design

A Job Object may need `golang.org/x/sys/windows`. Check first whether the standard library's `syscall` suffices; if an external module is added, write in the design why it beats the standard library.

## Completion criteria

- Windows CI passes the equivalent of `mise run verify`.
- A test that starts child processes leaves none behind after a timeout.
