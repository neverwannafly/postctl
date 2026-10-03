# UX-M2 — mouse inside text editors

Owner model: GPT-5.6 Sol (medium) · Branch: `ws/ux-m2-editor-mouse` · Worktree: `~/work/postctl-wt/ux-m2-editor-mouse`

## Goal

Plan §0 U3 (mouse click and drag in text). Every `vimtext` field (request and WebSocket fields, scripts, bodies, overlays, screen fields, the read-only response body) responds to the mouse like a GUI editor while keeping vim modes coherent. Also close the M1 follow-ups.

## Read first

`AGENTS.md`, `internal/tui/AGENTS.md`, `docs/briefs/UX0-overview.md`, plan §0 U1 and U3, the Key map in `docs/design-language.md`, `docs/terminal.md` (Shift/Option-drag native selection). Code: `internal/tui/vimtext` (`render.go`: gutter, `left`/`top` scroll, cluster widths, `Options.Mask`; `model.go`: `SetCursor`, `Select`, `Focus`, `FocusNormal`, `ScrollLines`), `internal/tui/vimfield`, `internal/tui/screenfield`, `internal/tui/mouse.go` (hit regions, `MouseHandler`, click-only and wheel-only rules, modal screens), `internal/tui/request/mouse.go`, `internal/tui/wspane/mouse.go`, `internal/tui/response/mouse.go` and `vimbody.go`, `internal/ui/header.go` (`TabRanges`, `TabAt`).

## Scope (files you own)

`internal/tui/vimtext/` (additive API only), `internal/tui/vimfield/`, `internal/tui/screenfield/`, the four `mouse.go` files and their tests, the mouse hooks in overlays (`pickinput.go`, prompt input) and screens that host fields (`envedit`, `runner`, `history` filter), `docs/keys.md` via `KeyCatalog` if a hint changes, `docs/design-language.md` (Mouse paragraph). In parallel: UX-QA (owns `internal/qa/`, `scripts/tui_smoke.py`, docs sweep). Do not edit `internal/qa/` except to append one case if QA asks through the ledger.

## Work

1. **vimtext hit mapping** (additive): `func (m *Model) PosAt(x, y int) (row, col int, ok bool)` maps a cell inside the widget's own frame to a buffer position, honouring the line-number gutter, `top`/`left` scroll, wide graphemes (a click on either cell of a wide cluster maps to that cluster), tabs (`TabText`), masked display (`Options.Mask`: one glyph per cluster), the placeholder (empty buffer maps to 0,0), and positions past the end of a line (clamp: normal mode to the last cluster, insert mode to EOL). Table tests for each.
2. **Click**: a left click inside a field focuses it (through the pane's normal focus path, so blur and commit of the previous field happen) and places the cursor. Mode rules: a field already in insert stays in insert at the new position; otherwise the field enters **normal** mode at the position (never starts insert on a click). The read-only response body: cursor moves, mode normal. A click on the URL bar does not send.
3. **Drag**: press, motion with the left button held, release → visual (charwise) selection from press to the current position; release keeps the selection in visual mode so `y` yanks it (masked as usual). Dragging past the top or bottom edge scrolls one line per motion event (`ScrollLines`). A press without motion is a click. Use the M1 rule set: only `MouseClickMsg` starts a press; track motion only while a press that started inside a field is active; a release anywhere ends it. Drag in a single-line field scrolls horizontally.
4. **Double-click** selects the word under the pointer (vim `viw` semantics, visual mode); **triple-click** selects the line (`V`). Use M1's injectable clock and 400 ms window.
5. **Wheel** inside a focused field scrolls it (`ScrollLines`) in every mode except while a drag is active; this refines M1/V2a's "wheel ignored while editing" rule only for the field under the pointer. Keep the guard for every other target.
6. **M1 follow-ups**: derive the tab-row Y offsets in `internal/tui/mouse.go` and `response/mouse.go` from layout constants instead of literals; make the breakpoint and ASCII mouse tests behaviour-level (a click lands on the intended pane or tab at 99/100 and 139/140 columns).
7. Docs: one Mouse paragraph in `docs/design-language.md` (click, drag, double and triple click, wheel; Shift/Option-drag for native terminal selection); `README.md` mouse line if present.

## Rules

`View` pure (no hit-map building in `View`; compute in `SetSize`/`Update` or derive from stored geometry). Masked fields never reveal on click or selection; a yank of a selection in a hidden secret gives the fixed mask. `--no-mouse` turns all of this off. No new globals.

## Tests

`PosAt` table (gutter, scroll, wide, tab, mask, past EOL, placeholder); click in each field kind (request URL, kv cell, body, script, WS composer, palette input, env editor cell, response body) puts the cursor at the expected row/col and the expected mode; click in another pane while in insert commits the edit; drag selection and edge scroll; double and triple click; wheel in focused field; release outside the field ends the drag; `--no-mouse` no-op. Purity tests for any new state. Goldens only if visible output changes (read the diffs). Keep `VIMTEXT_NVIM=1 go test ./internal/tui/vimtext -run TestDifferentialNvim` green.

## Acceptance

`task agents:check` green, `task tui:smoke` passes, `postctl --help` runs. Ledger STARTED/DONE + status row `UX-M2 mouse in editors`. Commit; do not push. Final message: API added, behaviours, tests, goldens changed (count), commit hash.
