# postctl — work ledger

Append-only. Every agent (and the orchestrator) adds an entry when it **starts**, at each **meaningful milestone**, when **blocked**, and when it **finishes**. Never edit or delete someone else's entry; add a correction entry instead.

## Entry format

```markdown
### <YYYY-MM-DD HH:MM IST> · <WS id> · <agent/model> · <STARTED|PROGRESS|BLOCKED|DONE|HANDOFF>
- Branch/worktree: ws/<n>-<name>
- Did: <what changed, in plain sentences>
- Files: <paths touched>
- Decisions: <interface or design decisions others must know; link to plan/brief section>
- Tests: <what was run, result>
- Blockers / questions: <for orchestrator>
- Next: <next step or "none">
```

## Status board

| WS | Owner (model) | Status | Branch | Last update |
| --- | --- | --- | --- | --- |
| Plan | Orchestrator | Draft v0, awaiting user answers | — | 2026-09-29 |
| WS0 scaffold/model/store | — | Not started | — | — |
| WS1 Postman importer | — | Not started | — | — |
| WS1b Postman local extractor | GPT-5.6 Sol | Started | ws/1b-postman-local | 2026-09-29 23:09 IST |
| WS2 vars + httpx | — | Not started | — | — |
| WS3 script runtime | — | Not started | — | — |
| WS4 engine/runner | — | Not started | — | — |
| WS5 UI theme + TUI shell | — | Not started | — | — |
| WS6 editor + response viewer | — | Not started | — | — |
| WS7 CLI | — | Not started | — | — |
| WS8 docs/release | — | Not started | — | — |

## Entries

### 2026-09-29 22:45 IST · Plan · Orchestrator · STARTED
- Did: Read `~/work/prod-console-tui` docs (design-language, tui-stack, plan, AGENTS, README; skipped okta.md as requested). Drafted `docs/plan.md` v0 with architecture, storage proposal, import mapping, script API tiers, workstreams and model assignments.
- Files: `docs/plan.md`, `docs/ledger.md`
- Decisions: Stack mirrors prod-console-tui (Bubble Tea/Bubbles/Lip Gloss/Huh v2); goja proposed as JS runtime (pure Go, no cgo).
- Blockers / questions: 15 open questions sent to user (plan §12).
- Next: Record answers, bump plan to v1, write per-workstream briefs, init git, launch Wave 1 (WS0).

### 2026-09-29 23:00 IST · Plan · Orchestrator · PROGRESS
- Did: Recorded answers to Q1–Q15 in plan §0 and bumped the plan to v1. Inspected the local Postman install: backups are empty and the real data lives in Chromium IndexedDB (LevelDB + V8-serialized values). Added WS1b (local extractor) and WS9 (WebSocket/gRPC).
- Files: `docs/plan.md`, `docs/ledger.md`
- Decisions: WS1b outputs standard v2.1 JSON so the importer (WS1) stays format-stable. Private extracted data goes to gitignored `testdata/private/`.
- Blockers / questions: Round-2 questions (gRPC/OAuth2 depth, launch approval).
- Next: Write briefs for WS0 and WS1b, then launch Wave 1.

### 2026-09-29 23:09 IST · WS1b · GPT-5.6 Sol · STARTED
- Branch/worktree: ws/1b-postman-local
- Did: Read the authoritative brief, plan §0 and Q15 findings, and ledger process. Began the read-only Phase A format spike.
- Files: `docs/ledger.md`
- Decisions: Real IndexedDB data will be copied to temporary storage before inspection; all diagnostics are restricted to non-sensitive structure, names, and counts.
- Tests: Not run yet.
- Blockers / questions: None.
- Next: Enumerate copied databases and object stores, identify record shapes and decoding feasibility, then document findings before Phase B.

### 2026-09-29 23:34 IST · WS1b · GPT-5.6 Sol · PROGRESS
- Branch/worktree: ws/1b-postman-local
- Did: Completed Phase A against a temporary copy of the active partition. Identified current `v3-collections` and `environments` stores plus the legacy `variable_sessions` globals source; documented key prefixes, value envelopes, record field names, relationships, ordering, scripts, and environment/global shapes.
- Files: `docs/postman-local-format.md`, `docs/ledger.md`, `go.mod`, `go.sum`
- Decisions: Pure-Go extraction is feasible. Use `goleveldb` with comparator name `idb_cmp1` for an unbounded sequential scan; decode the IndexedDB/Blink envelopes, Snappy marker, and the V8 tags present. Treat missing/partial cache records as explicit coverage gaps and retain Postman File → Export as fallback.
- Tests: A privacy-safe spike decoded current records without exposing values. Current live-cache probe found 2 root collections, 11 folders, 29 requests, 17 pre-request scripts, 0 test scripts, 3 environments, and no live globals record.
- Blockers / questions: The cache is partial. External blob values are identifiable but were not needed by current live collection/environment records.
- Next: Implement and test the package, staging writer, coverage report, and development CLI.
