# WS6c — TUI collection runner and history

Owner model: Composer 2.5 (fast) · Branch: `ws/6c-runner-history` · Worktree: `~/work/postctl-wt/ws6c-runner-history`

## Setup (do this first)

The orchestrator's shell is unavailable, so create your own worktree from `/Users/shubham/work/postctl`: `git worktree add -b ws/6c-runner-history ~/work/postctl-wt/ws6c-runner-history main`. Work only there.

## Goal

Two TUI screens on top of the merged engine: a **collection runner** (Postman's Runner) and a **history** browser. Both follow the design language exactly: calm, honest progress, and readable at 80×24.

## Read first

`docs/design-language.md` (binding), `docs/plan.md` §0 and §8, `docs/engine.md` (Run, RunInput, RunEvent types, RunSummary; history Entry/List/Get), `AGENTS.md`. Code: `internal/tui/pane.go` and `internal/tui/screens.go` (`Screen`, `ScreenEnv`, `ScreenFactory`, `OpenScreenMsg`), `internal/tui/envedit` and `internal/tui/cookies` (screen examples to mirror), `internal/tui/response` (reuse its body/tests rendering for history detail), `cmd/postctl/wire.go` (`lazyEngine`; you'll add `Options.NewRunner`/`NewHistory` wiring there), `internal/engine`, `internal/history`.

## Scope (files you own)

`internal/tui/runner/`, `internal/tui/history/`, additive `Options` fields plus palette/key registration in `internal/tui` (small, additive), and wiring lines in `cmd/postctl/wire.go`. You may need an engine accessor from the lazy engine; add it in `wire.go`. Other agents running in parallel: WS7 (CLI, `internal/cli` and dispatch in `main.go`) and WS9 (WebSocket). Don't edit their files.

## Runner screen (`R` on a collection/folder in the tree, or the palette "Run collection…")

- **Setup form:** scope (collection or folder, preselected from the tree), environment (defaults to active), iterations, data file (path input with CSV/JSON preview: first rows and column names), delay, bail on failure, and persist variables (on by default, per R3). The review line repeats the environment; production environments show the warning badge (Q10: badge only).
- **Live view:**
  - Iteration header, then one row per request: method badge, name, status chip, time, and `tests 3/3` or `× 1 failed`. The in-progress row shows a spinner with the step text.
  - A summary strip with counts and elapsed time.
  - Keys: `Esc` cancels (confirm), `p` pauses and resumes if feasible (skip it if the engine can't pause, and say so in the ledger), `Enter` opens a request's detail (response, tests, console) using the response pane's rendering, `f` shows failures only.
- **Summary:** the RunSummary as a readable table with a failures list. Actions: re-run, re-run failed requests, and export JSON/JUnit (reuse WS7's reporters if already merged; otherwise write JSON only and note it).
- Runs execute via `engine.RunAsync`/`Run` in a command, with events streamed into the Update loop race-free.

## History screen (`H` or palette "History")

- List of recent sends: time, method, status chip, request path, duration and test counts. Filter by text, status class and request. Grouped by day.
- **Detail:** the resolved request (secrets already redacted by history), response body and headers (reuse the response pane components), and tests.
- **Actions:** re-open the request in the editor, re-send it (as the saved request with current variables), copy as curl (redacted), and clear history (danger confirm).

## Tests

`Update` tests for both screens with a fake engine emitting scripted `RunEvent` sequences (including cancel, bail, script errors and transport errors), history filter and actions, and golden renders at 80×24, 120×40 and 200×60 in dark, NO_COLOR and ASCII. Also a check that no secret value appears in any frame.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green; design checklist results in the DONE ledger entry; ledger entries and a status row in your worktree's `docs/ledger.md`.

## Final message

What shipped, keys, text renders (runner live view and summary at 80×24 and 140×40), deviations, test/lint results, commit hash.
