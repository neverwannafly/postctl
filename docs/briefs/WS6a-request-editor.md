# WS6a — Request editor, environment editor, send wiring

Owner model: Sonnet 5.5 (high) · Branch: `ws/6a-request-editor` · Worktree: `~/work/postctl-wt/ws6a-request-editor`

## Goal

The Request pane: where users spend most of their time. They edit URL, params, auth, headers, body and scripts inline or in `$EDITOR`, then press one key to send. Plus the environment editor, the OAuth2 "Get new token" flow, the cookie manager, and shell-side send wiring (with a fake `Sender` until the engine lands).

## Read first

- `docs/design-language.md` (binding), `docs/tui-stack.md`, `AGENTS.md`, `docs/plan.md` §0 (especially Q5, Q10, Q11, R2, D5, D6), §8.
- `internal/tui/pane.go` (`Pane`, `Titled`, `Editor`, `ActionSource`, `Action`, `Registry`, `PaneEnv`, `PaneFactory`, `RequestRef`, and the shell↔pane messages), `internal/tui/app.go` (`Options.NewRequestPane`), `internal/tui/backend.go` (`SaveRequest`, `SaveEnvironment`), `internal/tui/placeholder.go` (what you replace).
- **Send contract:** `internal/tui/send.go` (`Sender`, `SendRequest`, `SendResult`, `SendRequestMsg`, `CancelSendMsg`, `SendStartedMsg`, `SendProgressMsg`, `SendResultMsg`). The orchestrator wrote it; extend it additively only. WS6b (response pane) consumes the same messages in parallel.
- `internal/model` (Request, URL, KV, Body, Auth incl. `OAuth2Config`, Scripts, Settings, Environment, Variable; `Clone`), `internal/store` (`PathOf` for `$EDITOR`, `Hydrate*`, `State`), `internal/vars` (`Replace` for variable previews), `internal/httpx` (`Curl`, `CookieJar`), `internal/oauth2` (`Fetch`, `FetchOptions`, `Source`; its DONE ledger entry spells out the TUI sequence for auth code).

## Scope (files you own)

- `internal/tui/request/`: the Request pane package implementing `tui.Pane` (+ `Titled`, `Editor`, `ActionSource`).
- `internal/tui/envedit/`, `internal/tui/cookies/`: environment editor and cookie manager overlays.
- Shell wiring in `internal/tui` for send: handle `SendRequestMsg` by running `Options.Sender` (add the field) in a command. Deliver `SendStartedMsg`/`SendProgressMsg`/`SendResultMsg` to all panes (use a channel-listening command or `tea.Program.Send`; your call, but it must be race-free). One in-flight send per request; `Esc`/`CancelSendMsg` cancels via context.
- A `FakeSender` in `internal/tui/tuitest` (or similar) that returns canned `SendResult`s with progress steps, used by tests and by `cmd/postctl` until WS4 provides the real one.
- In `cmd/postctl`: only the wiring `Options.NewRequestPane` and `Options.Sender` (fake for now, behind a clearly named constructor that WS4 will replace).
- Cleanup of WS5 stand-ins now that WS0.1 has landed: replace the reflect `deepCopy` in `internal/tui/backend.go` with `model.*.Clone`, call `store.LoadLenient` directly instead of probing by type assertion, and pass `--workspace` through `store.WithWorkspace` if it isn't already.
- `store.State`: additive field `Folded []model.ID` (D6), plus shell wiring to persist tree fold state.

WS6b owns `internal/tui/response/` and the `NewResponsePane` wiring. Don't edit those.

## Requirements

1. **URL bar:** method selector (common methods plus custom), URL text input with `{{var}}` highlighted (resolved preview on focus, e.g. a secondary line `→ https://api.staging…/v1/charges`, secrets masked), unresolved variables flagged in warning color with the text "unresolved". `Enter` in the URL bar sends. Pasting a curl command into the URL bar offers "Import as request" (parse with a small curl parser: method, URL, `-H`, `-d/--data*`, `-u`, `-F`, `--compressed`, `-k`).
2. **Tabs:** Params, Auth, Headers, Body, Pre-request, Tests, Settings. Counts in tab labels (`Headers 3`), and a dot on tabs with content.
   - **Params / Headers / urlencoded / form-data:** a key-value table using `ui` components, with add, delete, enable/disable toggle (`Space`), reorder, inline edit, description column when wide, and bulk-edit mode (`key: value` lines).
   - **Auth:** type selector. Fields per type from the model; "Inherit from parent" shows the effective parent auth and where it comes from. Secret fields are masked with a reveal toggle. For OAuth2: config form + **Get new token** action per the oauth2 sequence (auth URL shown as a copyable link, browser opened, calm waiting state with Esc to cancel, a success/failure status, the token saved via `oauth2.Source.Save`), plus token expiry shown.
   - **Body:** mode selector; raw editor (bubbles textarea) with language selector, JSON pretty-format action and validation hint; binary file picker (path input); GraphQL query + variables.
   - **Pre-request / Tests:** script editors (textarea, monospace, line numbers), with a snippet list for common idioms (set env var from response JSON, status check, response-time check).
   - **Settings:** follow redirects, max redirects, verify TLS, timeout.
3. **`$EDITOR` round-trip (Q5):** `e` on the Body/script/any tab opens the relevant file (`store.PathOf`, or for bodies a temp file with the right extension) in `$VISUAL`/`$EDITOR` (fallback `vi`) via `tea.ExecProcess`, then reloads the result into the editor on return. The inline editor is always available too.
4. **Autosave:** debounce edits (~500 ms after the last keystroke, and on blur) and save through `Backend.SaveRequest`. Send `DirtyMsg` while unsaved. Undo/redo (`Ctrl-Z`/`Ctrl-Y`) within the pane session, using `Clone` snapshots.
5. **Send keys (D5):** `Ctrl-Enter` sends when the terminal reports enhanced keyboard support (Bubble Tea v2 keyboard enhancements). `Ctrl-S` always sends, and so does `Enter` in the URL bar. The footer shows `Ctrl-Enter Send` when it's supported, otherwise `Ctrl-S Send`. While sending: `Esc Cancel`.
6. **Copy as curl:** palette action + `y` key, using `httpx.Curl` on the variable-resolved request with secrets **masked** by default; a separate "Copy as curl (with secrets)" palette action.
7. **Environment editor (`Ctrl-E` or palette):** create, rename, duplicate, delete (danger modal), toggle `production` (Q10 badge only), and a variables table with secret toggle. Secret values are masked and hydrate on reveal through `Hydrate`. Save via `Backend.SaveEnvironment`. Also globals and collection variables (a tab per scope).
8. **Cookie manager (palette "Cookies"):** list the jar by domain, view, delete, and clear domain/all (confirm).
9. Keep every screen usable at 80×24 (single pane layout: tabs collapse to a selector, tables drop the description column).

## Tests

`Update`-level tests for every tab's editing flows, autosave debounce (with fake clock), undo, `$EDITOR` round-trip (fake exec command), send wiring with the fake sender (progress order, cancel, one in-flight), curl paste import, OAuth2 flow with fake `Fetch`/browser, env editor secret masking. Golden renders at 80×24, 120×40 and 200×60 for each tab, in dark, NO_COLOR and ASCII.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green. `go run ./cmd/postctl --workspace internal/tui/testdata/demo`: edit a request, send with the fake sender, see progress steps, edit an environment. Run the design checklist and record the results in the DONE ledger entry. Ledger entries in your worktree.

## Final message

What shipped, contract additions (send.go, `Options`, `State`), key map additions, text renders at 80×24 and 140×40 of the Body and Auth tabs, deviations, test/lint results, commit hash.
