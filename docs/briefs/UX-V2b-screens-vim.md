# UX-V2b — vim editing in overlays, screens and lists

Owner model: Composer 2.5 (fast) · Branch: `ws/ux-v2b-screens` · Worktree: `~/work/postctl-wt/ux-v2b-screens`

## Goal

Every text input outside the request and WebSocket panes becomes a `vimtext` editor, and every list screen moves with vim motions (plan §0 U1). Inputs: command palette, request switcher, tree filter, prompts and pickers (new, rename, move, confirm-with-input), environment editor (names, variable keys and values), cookie manager fields, runner setup form (iterations, delay, data file path), history screen filter. Lists: environment editor, cookies, runner (setup fields, live rows, summary), history list.

## Read first

`AGENTS.md`, `internal/tui/AGENTS.md`, `docs/briefs/UX0-overview.md`, the **Key map** in `docs/design-language.md` (Text fields: "Overlay inputs open in insert ..."; Panes and lists), `docs/tui-stack.md` (vimtext). Code: `internal/tui/vimtext`, `internal/tui/textin`, `internal/tui/palette.go`, `switcher.go`, `overlays.go`, `tree.go` (filter), `app.go` (`insertActive`, overlay routing, always-keys in non-modal overlays), `internal/tui/envedit`, `internal/tui/cookies`, `internal/tui/runner`, `internal/tui/history`.

## Scope (files you own)

`internal/tui/palette.go`, `switcher.go`, `overlays.go`, the filter part of `tree.go`, `internal/tui/envedit/`, `internal/tui/cookies/`, `internal/tui/runner/`, and `internal/tui/history/` **only after** the orchestrator tells you H1 has merged (do it last; if not merged when you are otherwise done, skip it and say so). Plus `cmd/genkeys/main.go`, `docs/keys.md`, goldens of your packages. In parallel: V2a (request and WebSocket panes, `internal/tui/complete`, the `ForwardKeyMsg` contract in `pane.go`/`app.go`), M1 (mouse), T (terminal). Do not edit their files; keep any `app.go` change minimal.

## Behavior

- **Overlay inputs** (palette, switcher, filter, prompts): open in insert (`StartInsert`). `Esc` → normal, where `j`/`k` (and arrows, `Ctrl-n`/`Ctrl-p` in both modes) move through results and `Enter` picks; `Esc` or `q` in normal closes. In insert, `Enter` picks the highlighted result. The footer shows the mode. The always-keys behavior in non-modal overlays (`Ctrl-S`, `Alt-1/2/3`, `Ctrl-P`) stays exactly as now.
- **Screen fields** (env editor cells, cookie fields, runner form): `Enter` edits in normal, `i`/`a` edit in insert, `Esc` ladder (insert → normal → leave the field → screen's own Esc). Secret values in the env editor stay masked unless revealed, including inside the editor while not revealed (editing a hidden secret starts from an empty value, as today: check current behavior and keep it).
- **Lists**: `j`/`k`, `gg`/`G`, `Ctrl-d`/`Ctrl-u`, `/` filter, `n`/`N` where search exists, `Enter` opens. Destructive actions keep their confirm dialogs (default Cancel). Use one small shared helper for `gg` (two separate `g` presses) and `Ctrl-d`/`Ctrl-u` paging so all screens behave the same.
- Remove `textin` and `textinput` uses in your files. Delete `internal/tui/textin` only if nothing imports it after your change and V2a's (check `main`); otherwise leave it and note it.

## Tests

Per input: open, type, `Esc` to normal, `j`/`k` through results, `Enter` picks, `Esc`/`q` closes, `Ctrl-S` still sends from the palette and switcher. Env editor: edit name and value, reveal rules, secret never rendered unrevealed (extend the `internal/qa` leak sweep with an env-editor frame while a secret cell is being edited). Lists: `gg`/`G`/`Ctrl-d`/`Ctrl-u` per screen. Purity tests for each changed screen. Goldens at 80x24, 120x40, 200x60 in dark, `NO_COLOR`, ASCII; read every changed golden.

## Acceptance

`task agents:check` green, `task keys`, `task tui:smoke`. Ledger STARTED/DONE + status row. Commit; do not push. Final message: inputs and lists converted, anything skipped (history screen?), goldens changed (count), test results, commit hash.
