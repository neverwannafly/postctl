# UX-V3 — response pane: vim reading, per-request history, diff, jq

Owner model: Sonnet 5.5 (high) · Branch: `ws/ux-v3-response` · Worktree: `~/work/postctl-wt/ux-v3-response`

## Goal

Plan §0 U1, U5, U6. The response body becomes a read-only `vimtext` buffer (motions, search, visual select, masked yank). The response pane gains a **History** tab listing past sends of the current request, and two leader actions: **diff with previous** (`Space x`) and a **jq filter** (`Space j`). The leader already lists these action IDs (`internal/tui/leader.go`); today no registry provides them, so which-key hides them. You provide them.

## Read first

`AGENTS.md`, `internal/tui/AGENTS.md`, `docs/briefs/UX0-overview.md`, `docs/plan.md` §0 U1, U5, U6, the **Key map** and leader table in `docs/design-language.md`, `docs/tui-stack.md`. Code: `internal/tui/response/` (`pane.go` tabs `tabBody`..`tabTimeline`, `mask.go`, `reveal`, `virtual.go`, `jsonview.go`, `highlight.go`), `internal/tui/vimtext` (`Options.ReadOnly`, `Clipboard`, `Redact`, `Highlighter`; `Event` kinds), `internal/tui/diffview` (`New`, `NewHeaders`, `Compute`, `Cap`, `DiffTooBig`), `internal/tui/history/history.go` (`history.Filter{RequestID}`; how it opens entries and diffs), `internal/history` (`Entry`, `Redactor`), `internal/tui/leader.go` (action IDs `history.request`, `response.diff`, `response.jq`), `internal/tui/screens.go` (`openHistory`, `screenEnv(requestID)`, `screenActions`), `internal/tui/pane.go`, `internal/qa/secret_leak_test.go`.

## Scope (files you own)

`internal/tui/response/`, a new file `internal/tui/response_actions.go` for the three palette/leader actions (keep edits to `app.go`/`screens.go` to the one line that appends your actions), `cmd/postctl/wire.go` only if a factory needs a new dependency, `go.mod`/`go.sum` (gojq), `docs/tui-stack.md` (gojq row), `cmd/genkeys/main.go` + `docs/keys.md`, `internal/qa/secret_leak_test.go` (append cases only), goldens of your packages. In parallel: V2a (request and WebSocket panes; it adds `ForwardKeyMsg`/`ForwardSeqMsg` to `internal/tui/pane.go` and the shell handling), V2b (overlays, envedit, list screens including the history screen), M1 (mouse; `mouse.go` files are M1's), T (terminal, `cmd/postctl/terminal.go`).

## Order of work

Do 1–3 first; they do not depend on V2a. Do 4 after V2a merges into `main` (the orchestrator tells you; then `git merge main` into your branch), because the read-only editor must forward unhandled keys through V2a's `ForwardKeyMsg`/`ForwardSeqMsg`.

1. **History tab** (`gH` section jump, `Space H` = action `history.request`). A new response tab listing this request's past sends (newest first; method, status code **and** reason, duration, size, relative time; never color alone), loaded through the same history reader the history screen uses with `Filter{RequestID}` from a `tea.Cmd`. `Enter` opens the entry in the history screen detail (reuse `screenEnv(requestID)` / a typed message; do not duplicate the detail view). `d` on an entry diffs it with the current response. Empty state: "No sends of this request yet. Ctrl-S sends it." Reload after each send of this request. Bounded (last 200).
2. **Diff with previous** (`Space x`, `response.diff`). Diff the current response body (and headers via `NewHeaders`, toggled the way the history screen does) against the previous history entry of the same request, using `internal/tui/diffview` (run `Compute` in a `tea.Cmd`; a generation counter drops stale results; show the `DiffTooBig`/cap message). Both sides are **redacted** text: the current side goes through the pane's redactor exactly as the body view does; history is already redacted. Not available (action disabled with a reason toast) when there is no current response or no previous entry.
3. **jq filter** (`Space j`, `response.jq`). Add `github.com/itchyny/gojq` (pure Go; record it in `docs/tui-stack.md`). A single-line input above the body (use `vimtext` `SingleLine, StartInsert`); `Enter` applies; `Esc` follows the Esc ladder (insert → normal, then `Esc` in normal clears the filter and closes). Evaluate in a `tea.Cmd` with a context timeout (250 ms) and output cap (1 MiB, say "truncated"); errors show in one line without echoing body data. Only for JSON bodies (otherwise toast "jq needs a JSON body"). The filter runs on the **masked** JSON unless reveal is on; output is rendered through the same masking path. The filtered view is what `y` yanks and what the body tab shows until cleared; the tab label shows `Body (jq)`.
4. **Read-only vimtext body** (after V2a merge). Replace the body viewport with `vimtext` `ReadOnly` + `LineNumbers` + the existing highlighter (or `ChromaHighlighter` for JSON). Motions, `/` `n` `N`, `v`/`V`, `y`/`yy` copy via `tea.SetClipboard` (OSC 52); `Options.Redact` must mask unless the pane is in reveal mode; editing keys do nothing. Keep virtualization for large bodies: if a body exceeds the existing virtual threshold, keep the current virtual viewer and say "Large body: vim keys limited" in the footer rather than loading megabytes into the editor. Unhandled keys forward to the shell (leader, `g` section jumps, `Tab`, `Ctrl-hjkl`). `Esc` in normal leaves the body (Esc ladder). Mode label in the footer via `ModeReporter`.

## Rules

- `View` pure; every load, diff and jq evaluation runs in a `tea.Cmd` returning typed messages; nothing blocks `Update`.
- Nothing unmasked reaches the screen, clipboard, diff or jq output unless reveal is on. Extend `internal/qa/secret_leak_test.go`: sentinel secrets in a response body and headers must not appear in (a) a yank (capture `Clipboard`), (b) the diff frame, (c) jq output for a filter selecting the secret field, (d) the History tab frame. Prove each by temporarily removing your masking (say so in the ledger).
- No new globals; keys listed in `KeyCatalog` then `task keys`; help and footer show only wired keys.

## Tests

History tab: load, empty state, ordering, reload after send, `Enter`/`d` messages, stale load dropped. Diff: no-previous disabled, redacted sides, stale generation dropped, too-big message. jq: valid filter, syntax error, non-JSON body, timeout (a filter like `[range(1e9)]`), output cap, masked input. Read-only editor: motions/search/yank (masked and revealed), editing keys inert, forwarded keys reach the shell, large-body fallback. `TestViewIsPure` for each new state. Goldens at 80x24, 120x40, 200x60 in dark, `NO_COLOR`, ASCII; read every changed golden. `task tui:smoke` passes.

## Acceptance

`task agents:check` green, `go mod tidy` no diff, `task keys` committed, smoke passes. Ledger STARTED/PROGRESS/DONE + status row `UX-V3 response vim + history + diff + jq`. Commit; do not push. Final message: what shipped per item, goldens changed (count), leak cases added and proven, test results, commit hash.
