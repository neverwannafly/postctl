# WS6b — Response viewer

Owner model: Composer 2.5 (fast) · Branch: `ws/6b-response-viewer` · Worktree: `~/work/postctl-wt/ws6b-response-viewer`

## Goal

The Response pane: show what happened, fast and readably. Status, time, size, body (pretty, highlighted, foldable, searchable), headers, cookies, test results, console output and a timing timeline, plus honest in-progress and error states.

## Read first

- `docs/design-language.md` (binding; see "Response status", "Response viewer", syntax and status-class tokens), `docs/tui-stack.md`, `AGENTS.md`, `docs/plan.md` §0 and §8.
- `internal/tui/pane.go` (`Pane`, `Titled`, `ActionSource`, `Action`, `PaneEnv`, `PaneFactory`, and the shell↔pane messages), `internal/tui/placeholder.go` (the response placeholder you replace), `internal/ui` components (`ResponseChip`, tabs, status line, spinner, empty and error states) and `internal/ui/theme.go` (`StatusToken`, syntax tokens).
- **Send contract:** `internal/tui/send.go`. You consume `SendStartedMsg`, `SendProgressMsg` and `SendResultMsg`, and may send `CancelSendMsg`. `SendResult.Response` is `*httpx.Response` (`internal/httpx/types.go`). Extend the contract additively only. WS6a (request pane and shell send wiring) works in parallel and owns the shell side.

## Scope (files you own)

- `internal/tui/response/`: the Response pane package implementing `tui.Pane` (+ `Titled`, `ActionSource`).
- In `cmd/postctl`: only the `Options.NewResponsePane` wiring line.
- New dependencies: `github.com/alecthomas/chroma/v2` (lexers only; map its token types onto the theme's syntax tokens, never chroma's own styles) and optionally `github.com/tidwall/gjson` for path filtering.

## Requirements

1. **States:** nothing sent yet (empty state with the send hint); sending (spinner + current `SendStep` text + elapsed time, `Esc Cancel`); result; transport error (what happened, e.g. "Connection refused to api.local:8443", what to check, `Ctrl-S` Retry); script error (shown in addition to the response when present); skipped request.
2. **Summary line:** `ui.ResponseChip` (code + reason with status-class token, never color alone), time, size, and `Tests 3/4` in success or danger. Keep it on one line at 80 columns (drop secondary items first).
3. **Tabs:** Body, Headers (count), Cookies (count), Tests (passed/total), Console (count; warn/error counts highlighted), Timeline.
   - **Body:** auto-detect JSON / XML / HTML / text from Content-Type and sniffing. Views: Pretty (formatted + syntax highlighted), Raw, and Hex for binary (and a "binary, N bytes" note instead of garbage). JSON: fold/unfold objects and arrays (`←/→`, `Space`), line numbers, soft wrap toggle (`w`), search `/` with `n/N` and highlight, and a path filter (`.`-prefixed like `.data.items[0].id`, or `$.` JSONPath-lite) that shows the matching subtree. Truncated bodies show a clear note. Large bodies (≥ 5 MB) must stay responsive: format lazily and virtualize rendering to visible lines only.
   - **Headers:** table, with long values wrapped. **Cookies:** table (name, value masked if it looks like a session cookie until revealed, domain, path, expiry, flags).
   - **Tests:** `✓` / `×` / `-` rows with name and failure message; filter all/failed.
   - **Console:** level-labeled lines (`log`, `warn`, `error`) grouped by event (pre-request / tests).
   - **Timeline:** DNS, connect, TLS, TTFB and download as labeled bars with durations (ASCII fallback `###`), redirects list, remote address, TLS version/cipher, and the sent request (method, URL, headers) with secret-looking header values (Authorization, Cookie, X-Api-Key…) masked.
4. **Actions** (palette + keys): copy body (`y`), copy a value at the cursor path, save body to file (path prompt), open body in `$PAGER`/`$EDITOR` via `tea.ExecProcess`, toggle pretty/raw.
5. Keep the result per request ID: switching back to a request shows its last response for the session. It survives `WorkspaceReloadedMsg` by ID.
6. Usable at 80×24 (tabs collapse to a selector if needed). Honor NO_COLOR/ASCII/accessible (accessible: plain linear sections, no folding UI).

## Tests

Unit tests for content detection, JSON formatting/folding/search/path filter, virtualization (render cost doesn't grow with body size: benchmark a 10 MB JSON, first render < 50 ms), timeline math, and masking. `Update` tests for state transitions from the send messages. Golden renders at 80×24, 120×40 and 200×60 for each tab and state, in dark, NO_COLOR and ASCII.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green; `go run ./cmd/postctl --workspace internal/tui/testdata/demo` shows your pane (WS6a's fake sender may not be merged yet: add a test-only harness or a hidden `POSTCTL_DEMO_RESPONSE=1` fixture so the pane can be exercised); design checklist results in the DONE ledger entry; ledger entries in your worktree.

## Final message

What shipped, key map, text renders at 80×24 and 140×40 (JSON body, tests, timeline), perf numbers, deviations, test/lint results, commit hash.
