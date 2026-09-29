# WS9 — WebSocket requests

Owner model: Sonnet 5.5 (high) · Branch: `ws/9-websocket` · Worktree: `~/work/postctl-wt/ws9-websocket`

## Setup (do this first)

The orchestrator's shell is unavailable, so create your own worktree from `/Users/shubham/work/postctl`: `git worktree add -b ws/9-websocket ~/work/postctl-wt/ws9-websocket main`. Work only there.

## Goal

Postman-style WebSocket requests (plan Q8; gRPC is deferred to v1.1 per R1): connect to a `ws://`/`wss://` URL with headers, query params and auth, then send messages and watch a live, timestamped log of sent and received messages. Saved message templates sit alongside the request.

## Read first

`docs/plan.md` §0, `docs/model.md` (`Kind` has `websocket` reserved), `docs/design-language.md`, `docs/tui-stack.md` (the dependency policy prefers `github.com/coder/websocket`), `docs/engine.md`, `AGENTS.md`. Code: `internal/model/request.go`, `internal/store` (YAML schema; any model change needs golden updates and, if not additive, a migration), `internal/vars` (`ResolveRequest`), `internal/httpx` (auth application you can reuse for the handshake), `internal/tui` (`pane.go`, `app.go`: how the Request/Response panes are chosen), `internal/tui/request` (the HTTP pane, to mirror its URL bar and KV tables), and `cmd/postctl/wire.go`.

## Scope (files you own)

- `internal/model`: **additive** WebSocket fields only (e.g. `Request.WebSocket *WebSocketSettings{Messages []WSMessage{ID, Name, Body, Format text|json|binary-base64}, Subprotocols []string, PingInterval, ...}`), with store round-trip plus golden tests.
- `internal/wsx/`: the client. Dial with headers, auth and cookies from the jar; send, receive, close; ping/pong; event stream with timestamps, sizes and direction; close codes; reconnect on demand.
- `internal/tui/wspane/`: a Pane for `KindWebSocket` requests. URL bar with Connect/Disconnect, tabs (Messages, Params, Headers, Auth, Settings), a message composer (textarea with JSON format/validate), saved message list (send with `Enter`, save the composer content as a template), and a live log (virtualized, filter by direction, search, JSON pretty view of the selected message, and a copy action).
- `internal/tui`: a small additive hook so the shell uses your pane when the selected request's `Kind` is websocket (e.g. `Options.NewPaneForKind map[model.Kind]PaneFactory` or similar). The HTTP request pane stays the default.
- Tree and palette: "New WebSocket request" action; the method badge shows `WS`.
- `cmd/postctl/wire.go`: wiring lines only.

Pre-request/test scripts are **not** run for WebSocket requests (Postman doesn't either); variables resolve at connect time.

Parallel agents: WS7 (`internal/cli`, dispatch in `main.go`) and WS6c (`internal/tui/runner`, `internal/tui/history`, and their `wire.go` lines). Don't edit their files.

## Tests

`wsx` against an `httptest` WebSocket echo server: text and binary frames, ping, server close with a code, a handshake failure (401) surfaced with status and body, context cancel, and a race-clean concurrent send and receive. Pane `Update` tests with a fake client, and golden renders at 80×24, 120×40 and 200×60 in dark, NO_COLOR and ASCII. Store round-trip for the new fields.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green; ledger entries and a status row in your worktree's `docs/ledger.md`; `docs/model.md` updated for the new fields.

## Final message

Model additions, `wsx` API, pane keys, text renders at 80×24 and 140×40 (connected with a log), deviations, test/lint results, commit hash.
