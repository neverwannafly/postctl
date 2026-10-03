# UX-T — tmux and terminal integration

Owner model: Composer 2.5 (fast) · Branch: `ws/ux-t-terminal` · Worktree: `~/work/postctl-wt/ux-t-terminal`

## Goal

Plan §0 U2 and U4: optimise for Ghostty inside tmux without breaking other terminals.
1. `Ctrl-h/j/k/l` at postctl's edge moves to the neighbouring **tmux** pane (vim-tmux-navigator style).
2. On start-up inside tmux, check the settings postctl needs and warn **once**, non-modally, when one is off.
3. A user doc `docs/terminal.md` with the exact config for Ghostty and tmux, plus notes for iTerm2, kitty, WezTerm and Terminal.app.

## Read first

`AGENTS.md`, `docs/briefs/UX0-overview.md`, `docs/plan.md` §0 U2, U4, D5, the Key map in `docs/design-language.md`. Code: `internal/tui/app.go` (`Options.OnEdge`, status line / notice mechanism: find how the shell shows a transient status message), `internal/tui/panemove.go`, `cmd/postctl/wire.go`, `cmd/postctl/main.go`, `internal/tui/run.go` (program options; keyboard enhancements in Bubble Tea v2).

## Scope (files you own)

A new package `internal/termx` (no Charm imports needed; pure functions + an injectable command runner), `OnEdge` and notice wiring in `cmd/postctl/wire.go`, an additive `Options.Notice string` (or reuse an existing status mechanism) in `internal/tui/app.go` if needed (tiny diff; M1 and V2a also touch `app.go`), `docs/terminal.md` (new), links from `README.md` and `docs/keys.md` (via `cmd/genkeys`). Do not edit `main.go` flags (M1 adds `--no-mouse` there).

## Work

1. **Edge handoff**: when `$TMUX` is set, `OnEdge(dir)` runs `tmux select-pane -L|-D|-U|-R` via `exec.Command` (no shell, no string interpolation) in a `tea.Cmd`; errors are ignored silently except for a debug-free status message when tmux is missing. Not in tmux: do nothing. Do not handoff when the tmux pane is zoomed (`#{window_zoomed_flag}` = 1).
2. **Detection** (`internal/termx`): inside tmux, read `tmux show -gv` for `extended-keys`, `escape-time`, `mouse`, `set-clipboard`, and `display -p '#{client_termfeatures}'` if useful; outside tmux, identify Ghostty via `TERM_PROGRAM`/`TERM`. Recommended: `extended-keys on` (or `always`), `escape-time` ≤ 10, `mouse on`, `set-clipboard on`; also `terminal-features` containing `extkeys` for the outer terminal. Produce at most one short notice, e.g. `tmux: set extended-keys on and escape-time 0 for Ctrl-Enter and Esc (see docs/terminal.md)`; show it once per start in the status line (not a dialog), never block start-up (timeout 300 ms on the tmux calls, run in a `tea.Cmd`).
3. **docs/terminal.md**: tmux.conf block (extended-keys, `extended-keys-format csi-u` if the tmux version supports it, `terminal-features 'xterm-ghostty:extkeys'`, escape-time, mouse, set-clipboard, focus-events) and the vim-tmux-navigator snippet that adds `postctl` to the `is_vim` pattern so `Ctrl-h/j/k/l` reach postctl; Ghostty config notes (keybind passthrough for `ctrl+enter` if needed); what degrades on other terminals (Ctrl-Enter → use Ctrl-S; Ctrl-i → `Space ]`; Alt keys need "Option as Meta" in iTerm2/Terminal.app; clipboard via OSC 52 needs `set-clipboard on`). Every key mentioned must match `docs/keys.md`.

## Tests

`internal/termx`: table tests with a fake command runner for every setting combination (good, each bad, tmux missing, timeout), zoomed pane, Ghostty detection; `OnEdge` command construction (exact argv) for each direction; notice shown once. `internal/qa` docs reference test still passes. No network, no real tmux needed in tests.

## Acceptance

`task agents:check` green. Ledger STARTED/DONE + status row. Commit; do not push. Final message: behavior, the notice text, files, test results, commit hash.
