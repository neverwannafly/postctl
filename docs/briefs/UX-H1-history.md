# UX-H1 — history: restore and response diff

Owner model: Composer 2.5 (fast) · Branch: `ws/ux-h1-history` · Worktree: `~/work/postctl-wt/ux-h1-history`

## Goal

Plan §0 U5. Users can (a) restore a past send's request into the editor and (b) diff the responses of two history entries. Today a history entry stores only the **resolved, redacted** request (`history.RequestSnapshot`), which can't be restored safely: it would write resolved hosts and mask glyphs into the request file. So this workstream also records a redacted **template** (the unresolved request) on every new entry. The per-request History tab in the response pane and "diff with previous" are phase 2 (V3) and will reuse your diff component, so keep it reusable.

## Read first

`AGENTS.md` (Security and privacy, Architecture invariants, pitfall 2), `internal/tui/AGENTS.md`, `docs/briefs/UX0-overview.md`, `docs/plan.md` §0 U5, `docs/engine.md` (history section), `docs/model.md`. Code: `internal/history/history.go` (`Entry`, `RequestSnapshot`, `ResponseSnapshot`), `internal/history/redact.go` (`Redactor`: `Text`, `Headers`, `URL`, `Body`, `Sensitive`), `internal/engine/record.go` (where entries are built), `internal/engine/session.go` (`redactor`), `internal/tui/history/history.go` (the `H` screen: phases, detail tabs, actions `o`/`s`/`c`), `internal/tui/request/` (how edits go through `record` + autosave: `history.go`, `bind.go`), `internal/tui/pane.go`, `internal/qa/secret_leak_test.go`.

## Scope (files you own)

`internal/history/`, `internal/engine/record.go` (and a test file), `internal/tui/history/`, new package `internal/tui/diffview/`, an additive message type in `internal/tui/pane.go`, a **new file** `internal/tui/request/restore.go` plus a one-case hook in the request pane's message switch, `internal/qa/secret_leak_test.go` (extend), docs (`docs/engine.md`, `docs/model.md` history format, `docs/keys.md` via `task keys` if you add keys). V0 is rewriting key routing in `internal/tui/request/keys.go`, `app.go` and the history keys will move to vim/leader later: add your new history-screen keys in the screen's own key switch only, and keep request-pane changes to the new file plus the hook.

## Work

1. **Template on entries.** Add `Template *RequestTemplate` (`json:"template,omitempty"`) to `history.Entry`: method, URL, params, headers, body mode and raw text (or urlencoded/form-data key-values; file parts by name only), `BodyTruncated`. Built from the **unresolved** request in `record.go` and passed through the redactor. Sensitive-named fields (per `Redactor.Sensitive`) keep their value only when it is nothing but variable references with an optional scheme word (`^\s*((Bearer|Basic|Token)\s+)?(\{\{[^{}]+\}\}\s*)+$`); otherwise they are masked. Secret values the redactor knows are masked everywhere. Old entries without a template still load.
2. **Restore.** In the history detail view, an action **Restore into request**: confirm dialog (defaults to Cancel) naming what will be replaced (method, URL, params, headers, body) and what won't (auth, scripts, settings). On confirm the screen emits `tui.ApplyRequestPatchMsg{RequestID, Patch}` (new, in `pane.go`, documented) and opens the request. The request pane applies it through its normal edit path so request-level undo reverts it and autosave persists it. Any field that is masked or truncated in the template keeps the current value, and the status line says which (key names only, e.g. "Kept current: header Authorization, body (truncated)"). Entries without a template: restore is disabled with the reason shown. If the request no longer exists: offer **Restore as new request** in the original collection/folder (fallback: first collection), named `<name> (restored)`, created via the `Backend`.
3. **Diff.** In the list, `m` marks one entry (visible marker, not color alone); with a mark set, an action **Diff with marked** opens a diff view of the two **response** bodies (pretty-printed when both are JSON, else raw text), plus a header summary line (status, duration, size per side) and a toggle to diff response headers instead. `internal/tui/diffview` holds a line diff (Myers or patience; implement it in the package unless a pure-Go dependency already in `go.mod` does it) and a renderer: unified view with `+`/`-` prefixes and semantic add/remove tokens, hunk headers, `n`/`N` next/previous hunk, scroll, "No differences" empty state, a cap (e.g. 20,000 lines per side) with a stated "truncated" note. Everything it renders is already redacted history data; never pass `Outcome.Resolved` or unredacted bodies in.
4. Update the history screen's key hints and help, `KeyCatalog` + `task keys` for new keys (`m`, restore, diff), and docs: history format (`template` field) in `docs/engine.md`/`docs/model.md`, user-facing description in the TUI section of `README.md`.

## Tests

- `record.go`: template built from the unresolved request; table for the sensitive-field rule (pure references kept; literal tokens, mixed literal+reference, and known secret values masked); old JSONL without `template` still parses.
- Restore: patch applies method/URL/params/headers/body; masked and truncated fields keep current values and are named in the status; undo reverts; save emitted; missing request path creates a new request through a fake `Backend`; disabled without template.
- Diff: algorithm table (empty, identical, insert, delete, replace, moved block), JSON pretty-print both sides, cap, hunk navigation, purity test, goldens at 80x24, 120x40, 200x60 in dark, `NO_COLOR`, ASCII.
- **Leak sweep:** extend `TestSecretDoesNotLeakAcrossPersistenceReportsCurlOrTUI` so the sentinel secret goes through a template on disk, the restore status line, the restored request file, and diff frames; prove it by temporarily removing your masking (say so in the ledger).

## Acceptance

`task agents:check` green; goldens reviewed; ledger STARTED/DONE plus a status row; commit on your branch, do not push. Final message: format change summary, keys added, restore and diff behavior, test results, commit hash.
