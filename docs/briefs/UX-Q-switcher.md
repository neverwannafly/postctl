# UX-Q — request quick switcher

Owner model: Gemini 3.8 Flash (high) · Branch: `ws/ux-q-switcher` · Worktree: `~/work/postctl-wt/ux-q-switcher`

## Goal

A dedicated overlay that finds **requests only** and opens one in two or three keystrokes (plan §0 U6). Today `Ctrl-P` mixes requests with every action. V0 binds it to `Space f` and `Space Space`; you expose it as a palette action with ID `request.find` and a message `tui.OpenSwitcherMsg{}`.

## Read first

`AGENTS.md`, `internal/tui/AGENTS.md`, `docs/briefs/UX0-overview.md`, `docs/design-language.md` (Interaction language, Key map, empty states). Code: `internal/tui/palette.go` (fuzzy matching, the request list it builds at roughly lines 95-117, `reveal` + `syncSelection(true)`), `internal/tui/overlays.go`, `internal/tui/app.go` (`syncSelection`, overlay routing), `internal/tui/tree.go` (request paths), `internal/tui/tuitest`.

## Scope (files you own)

New `internal/tui/switcher.go` and `internal/tui/switcher_test.go`; in `internal/tui/pane.go` only the new `OpenSwitcherMsg` type; in `palette.go` one action `request.find` ("Find request", group "Navigation", `Keys: ""` — V0 fills in `Space f` once the leader exists; never advertise an unwired key); in `app.go` the minimum: handle `OpenSwitcherMsg`, and record recency inside `syncSelection(true)` (a small `recent []model.ID` MRU, depth 50, newest first, deduplicated). V0 is rewriting key handling in `app.go` in parallel: keep your `app.go` diff under ~25 lines and do not touch `onKey`/`globalKey`.

## Behavior

- Opens with an empty query in insert (a plain `textinput` for now; V2b swaps in `vimtext`). Each row: method (text, not color only), request name, dim `Collection › Folder` path, and the URL **path** only (strip scheme, host and query: never render query strings).
- Empty query: recently opened requests first (the MRU), then the rest in tree order. With a query: fuzzy match on name, method, folder path and URL path (reuse the palette matcher), MRU as a tie-breaker; highlight matched characters with the selection token and underline (not color alone).
- `Down`/`Up`, `Ctrl-n`/`Ctrl-p` move; `Enter` opens (reveal in tree, `syncSelection(true)`, focus the request pane); `Esc` closes. Show `n of m` count. WebSocket requests are included with their `WS` method label. Requests from files that failed to load (lenient load) are excluded.
- Empty workspace state: "No requests yet. Space a creates one." Fits 80x24, `NO_COLOR` and ASCII.

## Tests

Ranking table (empty query MRU order, query matches by name, method, path segment, folder; tie-break), opening a result changes selection and focus, MRU dedupe and depth, query strings never rendered (a request URL with `?token=SENTINEL` must not appear in any frame), purity test, goldens at 80x24, 120x40, 200x60 in dark, `NO_COLOR`, ASCII.

## Acceptance

`task agents:check` green (no `KeyCatalog` change: you add no key); ledger STARTED/DONE plus status row; commit on your branch, do not push. Final message: behavior summary, goldens added, test results, commit hash.
