# INT3 — Merge WS9, then QA fixes

Owner model: GPT-5.6 Sol (medium) · Works in `/Users/shubham/work/postctl` on `main` for the merge, then on branch `qa/fixes` in worktree `~/work/postctl-wt/qa-fixes` for the fixes (the orchestrator's shell is unavailable).

## State

- `main` is clean at `a9761cc` (INT2), except for uncommitted orchestrator briefs in `docs/briefs/` (`WS8-docs.md`, `INT3-*.md`). Commit them first: `docs: WS8 and INT3 briefs`.
- `ws/9-websocket` at `7c4baa3` (worktree `~/work/postctl-wt/ws9-websocket`) is finished. `git merge-tree` shows only a `docs/ledger.md` conflict.
- `ws/8-docs` (WS8) is **still running: don't merge it or touch `README.md`, `docs/keys.md`, `examples/`**.
- Ledger resolver: `python3 /Users/shubham/work/postctl-wt/.orchestrator/resolve_ledger.py docs/ledger.md "<row prefix>" "<full new row>"`.

## Part A — merge WS9 on `main`

Check scope (`git diff --name-only main...ws/9-websocket`: model, store goldens, `internal/wsx`, `internal/tui/wspane`, `internal/tui/kindrouter.go` + small shell/tree/palette edits, `internal/httpx` (additive `ApplyAuth`), `cmd/postctl/wire.go`, `docs/model.md`, ledger, go.mod/sum). Merge `--no-ff`, run the full check, set the WS9 row to merged, remove the WS9 worktree, commit.

## Part B — QA fixes (branch `qa/fixes` from the new `main`; commit per item)

Orchestrator decisions to implement:

1. **WebSocket layout:** when the selected request is `KindWebSocket`, the shell hides the Response slot and gives the WebSocket pane the combined Request+Response width (three-pane → tree + WS pane; two-pane → WS pane fills the right side; single pane unchanged). Pane focus keys and help reflect the hidden slot. Add golden renders at 140×40 and 200×60.
2. **WebSocket requests in runs:** `engine.Run` (and so the TUI runner and `postctl run`) skips `KindWebSocket` requests with an `Outcome.Skipped`-style status and the reason "WebSocket requests are not run". They aren't counted as failures or requests executed, but are listed as skipped in summaries (runner summary, CLI summary table, JSON/JUnit as `skipped`). `postctl send` on a WebSocket request exits 3 with "Open WebSocket requests in the TUI; `postctl send` supports HTTP requests." Tests for each.
3. **Views must be pure:** `internal/tui/envedit`, `internal/tui/cookies` and the request pane's textarea adjust scroll offsets inside `View`. Move all state mutation into `Update`/`SetSize` (keep the rendered output identical: golden tests must pass unchanged), and add a test that calls `View` twice and asserts the model is unchanged (deep-equal on the relevant fields). Grep the rest of `internal/tui/**` for the same pattern and fix any other instances.
4. **Shared reporters:** move the CLI's JUnit (and JSON) run reporters from `internal/cli` into a new `internal/report` package (no behavior change; CLI golden tests unchanged). Use it for the TUI runner's summary export: the export path prompt accepts a `.xml` suffix for JUnit, `.json` for JSON, with a format toggle.
5. **Secret-leak sweep:** add one repository-level test (`internal/qa` or similar, test-only) that builds a workspace with a secret variable (unique sentinel value), a bearer token referencing it and an OAuth2 token. It sends a request through the engine to an `httptest` server that echoes headers, runs a collection run, exports JSON/JUnit and copies curl (masked), and renders the TUI golden-style at 140×40 for the request, response (Timeline tab) and history screens. It then asserts the sentinel appears **nowhere** in history files, `.postctl/`, YAML files, reporter outputs, masked curl, or rendered frames. The echoed response body is exempt, because the server legitimately returns it; the sentinel must still be absent from the history file, since history truncates or redacts headers. Fix whatever it finds.

## Rules

Small commits with clear messages. Full `go build ./... && go test -race ./... && golangci-lint run` green before every commit on `qa/fixes`. Don't merge `qa/fixes` into `main`; the orchestrator does that after an independent review. Never print secrets. Ledger: add a `QA-fixes` status row and STARTED/DONE entries on the `qa/fixes` branch.

## Final message

Part A result, and per item in Part B what changed, which tests were added, and anything the leak sweep found. Then `git log --oneline main..qa/fixes`.
