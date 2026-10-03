# UX-QA — leak sweep, smoke, goldens and docs for the vim/mouse/history work

Owner model: Sonnet 5.5 (high) · Branch: `qa/ux` · Worktree: `~/work/postctl-wt/qa-ux`

## Goal

Close the UX programme (plan §0 U1–U6) with an independent pass: prove no new output path leaks a secret, extend the TUI smoke script to the new flows, review goldens, and make the docs match the shipped keys. You find and fix; anything large goes to the ledger as a finding for the orchestrator.

## Read first

`AGENTS.md` (Security and privacy, Testing patterns, pitfalls 2, 5, 6, 7), `internal/tui/AGENTS.md`, `docs/briefs/UX0-overview.md`, plan §0 U1–U6, `docs/design-language.md` (Key map, leader table), `docs/keys.md`, `docs/terminal.md`, `README.md`, `docs/onboarding.md`, the UX ledger entries in `docs/ledger.md` (search `UX-`), `internal/qa/secret_leak_test.go`, `scripts/tui_smoke.py`.

## Scope (files you own)

`internal/qa/`, `scripts/tui_smoke.py`, `Taskfile.yml` (smoke target only), docs (`README.md`, `docs/keys.md` via `KeyCatalog` in `cmd/genkeys/main.go`, `docs/design-language.md`, `docs/terminal.md`, `docs/onboarding.md`, `docs/tui-stack.md`, `docs/architecture.md`), small fixes anywhere in `internal/tui/**` for findings you prove with a failing test (keep each fix minimal and separate). In parallel: UX-M2 owns `internal/tui/vimtext`, `vimfield`, `screenfield` and the `mouse.go` files; do not edit those (ask through the ledger).

## Work

1. **Leak sweep audit.** List every output path added by the UX work: vimtext yank in every field kind (request, WS, overlays, env editor, runner, history filter, response body, jq view, folded JSON line), completion menus (request and WS, every scope), history restore (masked fields keep current values; nothing masked written to disk), history diff and the response diff, the History tab, jq output and errors, the which-key popup and switcher (variable-derived text), `$EDITOR` temp files, toasts and footer notes, mouse-selected text. For each, confirm a case exists in `internal/qa` that fails when its masking is stubbed (use a `git clone` scratch copy, never `cp -R` a worktree). Add missing cases. Write the table (path → test → stub proven) into the ledger.
2. **Known follow-up**: the request pane leaves the `{{var}}` completer on secret fields (the WS pane disables it). Make the request pane match, with a test.
3. **Smoke script.** Extend `scripts/tui_smoke.py` (still offline, synthetic workspace, prints PASS/FAIL only, never data): open the switcher (`Space Space`), pick a request, edit the URL in insert mode and `Esc`, `Ctrl-S` sends, `gr` jumps to the response body, `Space H` opens the History tab, `Space x` diff shows or toasts, `Space j` jq filter with `.` applies, `Ctrl-h`/`Ctrl-l` move panes, `q` quits. Keep it under 30 s.
4. **Golden review.** Render a sample of goldens across packages at 80x24 in dark, `NO_COLOR` and ASCII; look for: colour-only meaning (method, status, mode label must have text), clipped footers advertising unwired keys, mode label present when a field is focused, `! prod` badge and active environment always visible. Fix with tests; regenerate only affected packages and read the diffs.
5. **Docs.** `docs/keys.md` regenerated and matching code (`go test ./cmd/genkeys ./internal/tui -run Key`); `README.md` key section and a short "vim keys" quick start; `docs/onboarding.md` mentions `Space` (which-key), `Space Space`, `Space h/H`, `g` jumps, `Ctrl-hjkl`, mouse and `--no-mouse`, `docs/terminal.md`; no doc still says "no vim bindings" or describes removed widgets (`textin`, Bubbles `textarea` in panes). `internal/qa` docs reference tests stay green.
6. **Dependency and layering check**: `go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/...` respects the AGENTS.md direction (engine imports no tui/ui/charm); `go mod tidy` no diff; every new dependency recorded in `docs/tui-stack.md`.

## Acceptance

`task agents:check` green, `task tui:smoke` passes with the new steps, `postctl --help` runs. Ledger STARTED/DONE with the leak-path table, findings fixed, findings deferred (with reason); status row `UX-QA final pass`. Commit; do not push. Final message: leak table summary (paths, cases added, stubs proven), smoke steps, golden findings, docs changed, commit hash.
