# UX-V2a — vim editing in the request and WebSocket panes

Owner model: Sonnet 5.5 (high) · Branch: `ws/ux-v2a-request` · Worktree: `~/work/postctl-wt/ux-v2a-request`

## Goal

Every text field in the request pane and the WebSocket pane becomes a `vimtext` editor (plan §0 U1, U6): URL bar, key-value cells (params, headers, path variables, urlencoded and form-data), auth and settings form fields, raw and GraphQL bodies, pre-request and test scripts, the WebSocket composer and its URL. Add syntax highlighting (JSON body, JS scripts), `{{var}}` completion in every field, and `pm.*` completion in scripts.

## Read first

`AGENTS.md`, `internal/tui/AGENTS.md`, `docs/briefs/UX0-overview.md`, `docs/plan.md` §0 U1, U6, D5, the **Key map** in `docs/design-language.md` (Text fields; Escape ladder), `docs/tui-stack.md` (vimtext section). Code: `internal/tui/vimtext` (API, `Event` kinds, `Options`), `internal/tui/textin`, `internal/tui/request/{pane,keys,texted,kv,form,auth,body,script,editor,history,bind}.go`, `internal/tui/wspane/{pane,keys,messages}.go`, the shell routing in `internal/tui/app.go` (`onKey`, `editing`, `insertActive`, `globalKey`, leader/prefix code in `leader.go`), `internal/tui/pane.go` (`Editor`, `ModeReporter`), `internal/vars` (scope chain, `FolderVariables`), `internal/script/assets/sandbox/*.js` and `docs/scripting.md` (the `pm.*` surface).

## Scope (files you own)

`internal/tui/request/`, `internal/tui/wspane/`, a new package `internal/tui/complete/` (completers), the key-forwarding contract in `internal/tui/pane.go` + its handling in `internal/tui/app.go` (keep that diff small and focused), `cmd/genkeys/main.go` + `docs/keys.md`, `docs/keys.md`-related README lines, goldens of your packages. In parallel: V2b (`envedit`, overlays, palette, filters, list screens), M1 (mouse: new `mouse.go` files in your packages are M1's — do not create files with that name), T (terminal integration in `cmd/postctl`). H1 (history) may merge while you work; it adds `internal/tui/request/restore.go` and a hook in the pane's message switch — keep it working.

## Work

1. **Key forwarding contract.** `vimtext` reports `Unhandled` (with the key) and `Seq` (e.g. `gu`) for keys it does not own in normal mode (`Space`, `:`, `q`, `Tab`, `Ctrl-o`, `Ctrl-h/j/k/l`, `g`+letter). Add to `pane.go`: `ForwardKeyMsg{Key tea.KeyPressMsg}` and `ForwardSeqMsg{Seq string}` (doc comments). A pane returns them as commands; the shell handles them exactly as if the key had arrived while no text field was inserting (global keys, leader, prefixes, section jumps). The shell's "typing" gate must use the mode: a field in **normal** mode does not block the leader; **insert** and **visual** do. Implement `ModeReporter.VimMode()` in both panes from the focused editor.
2. **Replace widgets.** URL bar and every single-line field: `vimtext` with `SingleLine` (`Enter` sends from the URL bar per D5; in other cells `Submit` commits the cell). Bodies, scripts, composer: multi-line with line numbers for scripts. Entering a field: `Enter` starts in normal, `i`/`a`/`A`/`I` on a row start in insert (Key map). `Esc` in normal leaves the field (`Leave`). Request-level undo (`u`/`Ctrl-r` outside fields) must still work: each field commit records one request-level step; inside a field, vimtext's own undo applies. Autosave and `BlurCmd` flush keep working. `Space o` (`$EDITOR`) works for every field kind, including the URL bar and single-line cells.
3. **Highlighting.** `vimtext.ChromaHighlighter` with `json` for JSON bodies (by content type or body mode), `javascript` for scripts; none for text.
4. **Completion** (`internal/tui/complete`). `Vars(scope)`: typing `{{` lists variable names from the resolved scope chain (global < collection < folder < environment < local; same precedence as `vars` and the engine) with the source scope in `Detail`; dynamic variables (`$guid`, `$timestamp`, ...) too. Secret variables show `••••` (ASCII `****`) in `Detail`, never the value. Accepting inserts `name}}`. `PM()`: in scripts, after `pm.` and deeper (`pm.environment.`, `pm.response.`, `pm.expect(`...), list members from a static table built from `docs/scripting.md`; keep the table in one file with a test that every documented member appears.
5. **WebSocket pane**: same treatment for its URL, params, headers, auth, settings fields and composer (JSON highlighting when the format is JSON).
6. **Remove** the old `textin`/`textarea` usage in your packages; leave the packages themselves for V2b's users until V2b finishes (delete them only if nothing imports them after both merge; note it in the ledger).
7. Update `KeyCatalog` (field-level keys point to the design-language map), help, footer hints (mode label first; only valid hints).

## Tests

Pane `Update` tests: entering and leaving each field kind, `Space`/`:`/`q`/`gb` from a field in normal mode reach the shell (shell-level test through `tuitest`), the same keys typed in insert mode become text, `Ctrl-S` sends from insert, normal and visual, `Enter` in the URL bar sends in both modes, `Esc` ladder (insert → normal → leave → cancel send), request-level undo after field edits, autosave emitted, `$EDITOR` round-trip for the URL. Completion: precedence table (folder beats collection, environment beats folder, local beats all), secret masked in `Detail` and in rendered frames (add a frame to the `internal/qa` leak sweep with the completion popup open on a secret variable), `pm.*` table. Purity tests (`TestViewIsPure`, `TestTextareaViewIsPure` successors). Goldens at 80x24, 120x40, 200x60 in dark, `NO_COLOR`, ASCII; read every changed golden. `task tui:smoke` passes.

## Acceptance

`task agents:check` green, `task keys` committed, smoke passes. Ledger STARTED/DONE + status row. Commit; do not push. Final message: fields converted (list), contract added, completion behavior, goldens changed (count), test results, commit hash.
