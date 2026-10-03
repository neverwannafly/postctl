# UX0 — vim keys, mouse, switcher, history (overview)

Orchestrator: Opus. Decisions: `docs/plan.md` §0 **U1–U6** (2026-10-03) and the [design-language key map](../design-language.md#key-map). Both are binding; if your work needs a change, write it under "Blockers / questions" in the ledger and take the least surprising option.

## Why

The user found the TUI slow to drive: pane keys were invisible, switching requests went through the mixed palette, and the two editing widgets (custom `textin` single-line, Bubbles `textarea` multi-line) behave differently. The answer is one modal vim editor everywhere, vim navigation, a `Space` leader with which-key, mouse, a request switcher, and richer history.

## Workstreams

| Phase | ID | Brief | Branch / worktree | Model | Depends on |
| --- | --- | --- | --- | --- | --- |
| 1 | V0 | `UX-V0-shell-keys.md` | `ws/ux-v0-shell` · `~/work/postctl-wt/ux-v0-shell` | Sonnet 5.5 (high) | — |
| 1 | V1 | `UX-V1-vimtext.md` | `ws/ux-v1-vimtext` · `~/work/postctl-wt/ux-v1-vimtext` | GPT-5.6 Sol (medium) | — |
| 1 | Q | `UX-Q-switcher.md` | `ws/ux-q-switcher` · `~/work/postctl-wt/ux-q-switcher` | Gemini 3.8 Flash (high) | — |
| 1 | H1 | `UX-H1-history.md` | `ws/ux-h1-history` · `~/work/postctl-wt/ux-h1-history` | Composer 2.5 (fast) | — |
| 2 | V2a | `UX-V2a-request-vim.md` | `ws/ux-v2a-request` | Sonnet 5.5 (high) | V0, V1 |
| 2 | V2b | `UX-V2b-screens-vim.md` | `ws/ux-v2b-screens` | Composer 2.5 | V0, V1, Q |
| 2 | V3 | `UX-V3-response.md` | `ws/ux-v3-response` | Sonnet 5.5 (high) | V0, V1, H1; step 4 after V2a |
| 2 | M1 | `UX-M1-mouse.md` | `ws/ux-m1-mouse` | Gemini 3.8 Flash (high) | V0 |
| 2 | T | `UX-T-terminal.md` | `ws/ux-t-terminal` | Composer 2.5 | V0 |
| 3 | M2 | `UX-M2-editor-mouse.md` | `ws/ux-m2-editor-mouse` | GPT-5.6 Sol (medium) | V2a, V2b, M1 |
| 3 | QA | `UX-QA.md` | `qa/ux` | Sonnet 5.5 (high) | all |

Phase 2 briefs V2a, V2b, M1, T written 2026-10-03 after V0, V1, Q merged; V3 waits for H1 (it reuses `internal/tui/diffview`). H1 merged 20:16; V3 brief written then, moved to Sonnet because its masking surface is wide. All phase 2 merged by 22:05; phase 3 briefs written then. M2 moved to GPT-5.6 Sol (it maps screen cells to vimtext positions; that model built vimtext). Review experiments use `git clone`, never `cp -R` of a worktree. Reviews: two reviewer subagents per workstream; the H1 final round moved from Composer to GPT-5.6 Sol after two failed algorithm rounds.

## File ownership (phase 1)

- **V0**: `internal/tui/*.go` (root package: `app.go`, `keys.go`, `help.go`, `layout.go`, `sidebar.go`, `tree.go`, `palette.go`, `pane.go` additive, new `leader.go`, `whichkey.go`, `jumplist.go`), key-routing edits in `internal/tui/request/keys.go`, `internal/tui/response/pane.go` (key handling only), `internal/tui/wspane/keys.go`, `cmd/genkeys/main.go`, `docs/keys.md`, `README.md` key section, root-package goldens.
- **V1**: `internal/tui/vimtext/` (new), `docs/tui-stack.md` (one section).
- **Q**: `internal/tui/switcher.go`, `internal/tui/switcher_test.go` (new); one palette action and one exported opener; a two-line hook where the shell opens a request (to record recency).
- **H1**: `internal/history/`, the history append site in `internal/engine/`, `internal/tui/history/`, new `internal/tui/diffview/`, an additive message in `internal/tui/pane.go` and its handler in `internal/tui/request/` (a new file), `docs/engine.md`/`docs/model.md` history sections.

Overlaps are deliberate and small (`app.go`, `pane.go`). Merge order: V1, Q, H1, then V0 (V0 rebases and regenerates keys and goldens last).

## Shared contracts introduced in phase 1

- `tui.FocusSectionMsg{Section string}` (V0): sent to panes by `g` jumps; section names `url params auth headers body pre tests settings messages resp-body resp-history`.
- `tui.SectionStepMsg{Delta int}` (V0): `[` / `]`.
- Leader dispatch (V0): leader keys map to `Action.ID`s from the focused pane's `Registry`; a pane exposes an action by registering it (IDs listed in V0's brief). Missing action = key hidden from which-key.
- `tui.ModeReporter` (V0, optional interface on panes): `VimMode() string` returns `NORMAL`, `INSERT`, `VISUAL`, `VISUAL LINE` or `""`.
- `vimtext.Model` (V1): API in `UX-V1-vimtext.md`.
- `tui.OpenSwitcherMsg{}` (Q): opens the request switcher.
- `tui.ApplyRequestPatchMsg` (H1): restore a history template into the open request through the pane's normal edit path (undoable, autosaved).

## Process (every workstream)

1. Create the worktree: `git worktree add ~/work/postctl-wt/<name> -b <branch> main` from `/Users/shubham/work/postctl`; work only there. Do not push.
2. Ledger: STARTED entry and a status-board row in your worktree's `docs/ledger.md`; DONE entry at the end. Counts and names only.
3. Gate before done: `task agents:check` green; goldens you changed regenerated and the diff read; `task keys` if keys changed.
4. Commit on your branch. Your final message: what shipped, deviations from the brief, test and lint results, commit hash.
5. The orchestrator dispatches two reviewers (correctness against the brief; security and invariants). Expect a follow-up with findings to fix.
