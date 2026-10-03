# UX-M1 — mouse support (panes, tree, tabs, wheel)

Owner model: Gemini 3.8 Flash (high) · Branch: `ws/ux-m1-mouse` · Worktree: `~/work/postctl-wt/ux-m1-mouse`

## Goal

Plan §0 U3: mouse on by default; click to focus a pane, click a tree item to select it (double-click or click on a selected item opens it), click a section tab to switch to it, wheel scrolls the pane under the pointer. `--no-mouse` / `POSTCTL_NO_MOUSE=1` turns it off. Keyboard-only stays complete. Clicking and dragging inside text editors is phase 3 (M2), not this workstream.

## Read first

`AGENTS.md`, `internal/tui/AGENTS.md`, `docs/briefs/UX0-overview.md`, `docs/plan.md` §0 U3, U4, `docs/design-language.md` (Interaction language). Code: `internal/tui/run.go`, `app.go` (`View`, `Update`, focus, `setFocus`), `layout.go` and `panemove.go` (V0 records pane geometry for `Ctrl-h/j/k/l`: reuse it), `tree.go`, `pane.go`, `kindrouter.go` (already forwards `tea.MouseMsg`), `internal/ui` tab component, the panes' tab bars (`request/pane.go`, `response/pane.go`, `wspane/pane.go`: read only), `cmd/postctl/main.go` (global flags), `cmd/postctl/wire.go`. Bubble Tea v2 mouse API: mouse mode is set on the returned `tea.View` (`MouseMode`), messages are `tea.MouseClickMsg`, `tea.MouseReleaseMsg`, `tea.MouseWheelMsg`, `tea.MouseMotionMsg`.

## Scope (files you own)

`internal/tui/run.go`, mouse routing in `internal/tui/app.go` (a focused addition; V2a also touches `app.go` for key forwarding), a new `internal/tui/mouse.go` (hit testing, double-click timing with an injectable clock), tree click handling in `tree.go`, the optional interface in `pane.go`, **new files named `mouse.go`** in `internal/tui/request/`, `internal/tui/response/`, `internal/tui/wspane/` implementing it (do not edit other files in those packages; V2a and V3 own them), `internal/ui` if the tab component needs to expose tab hit ranges, `Options.Mouse` + `--no-mouse` flag + `POSTCTL_NO_MOUSE` in `cmd/postctl/main.go`/`wire.go` and `ApplyEnv`, docs (`docs/cli.md` flag, `README.md`, a short "Mouse" note in `docs/keys.md` via `cmd/genkeys`).

## Work

1. Enable cell-motion mouse mode in `View` when `Options.Mouse` is true (default true; flag and env turn it off). `View` stays pure.
2. Hit regions: compute pane rectangles (sidebar, request, response, overlays) in `Update`/`SetSize` from the layout (reuse V0's geometry), never in `View`. Clicks inside an open overlay go to it; outside a modal overlay are ignored; outside a non-modal overlay close it.
3. Optional interface in `pane.go`: `MouseHandler { HandleMouse(msg tea.MouseMsg, x, y int) tea.Cmd }` with coordinates relative to the pane. Shell: left click focuses the pane, then calls `HandleMouse`; wheel goes to the pane under the pointer without changing focus.
4. Tree: click selects (and moves the cursor), double-click (≤ 400 ms, same row) or click on the already-selected row opens; click on a folder's fold glyph toggles it; wheel scrolls.
5. Panes (`mouse.go` in each): a click on a section tab sends the same message as `[`/`]` or `FocusSectionMsg`; wheel scrolls the body/viewer the same way `Ctrl-d`/`Ctrl-u` (or line scroll) does. Use only exported or package-internal methods that already exist; if a pane lacks a scroll method you need, add the smallest one in `mouse.go`.
6. Docs: note that with mouse on, native terminal selection needs a modifier (Ghostty: Shift-drag; iTerm2: Option-drag; tmux: Shift-drag) — put it in `docs/keys.md` (via genkeys) and README.

## Tests

Hit testing in each layout (three-pane, two-pane stacked, single-pane) at 80x24, 120x40 and 200x60; click focus; tree select, double-click open with a fake clock, fold toggle; tab clicks per pane; wheel routing to the pane under the pointer; overlay click rules; `--no-mouse` and env off (View has no mouse mode); `View` purity with mouse state. No goldens should change except where a deliberate visible change exists (there should be none).

## Acceptance

`task agents:check` green, `task keys`, `task tui:smoke`. Ledger STARTED/DONE + status row. Commit; do not push. Final message: behavior, files, test results, commit hash.
