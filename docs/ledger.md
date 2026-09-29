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
| Plan | Orchestrator | v1 locked | main | 2026-09-29 |
| WS0 scaffold/model/store | Sonnet 5.5 (high) | Done, merged to main | ws/0-scaffold | 2026-09-29 23:58 IST |
| WS1 Postman importer | GPT-5.6 Sol (medium) | Running | ws/1-importer | 2026-09-30 00:10 IST |
| WS1b local Postman extractor | GPT-5.6 Sol (medium) | Done, merged to main | ws/1b-postman-local | 2026-09-29 23:55 IST |
| WS2 vars + httpx | GPT-5.6 Sol (medium) | Running | ws/2-vars-httpx | 2026-09-30 00:10 IST |
| WS2b OAuth2 | Grok 4.7 (high, fast) | Running | ws/2b-oauth2 | 2026-09-30 00:10 IST |
| WS0.1 lenient load + Clone | Sonnet 5.5 (resumed WS0) | Running | ws/0-scaffold | 2026-09-30 00:10 IST |
| WS3 script runtime | Opus 5 Thinking (high) | Running | ws/3-script | 2026-09-30 00:10 IST |
| WS4 engine/runner | — | Not started | — | — |
| WS5 UI components + TUI shell | Sonnet 5.5 (high) | Done, ready to merge | ws/5-tui-shell | 2026-09-30 01:20 IST |
| WS6 editor + response viewer | — | Not started | — | — |
| WS7 CLI | — | Not started | — | — |
| WS8 docs/release | — | Not started | — | — |
| WS9 WebSocket requests | — | Not started (Wave 5; gRPC deferred to v1.1) | — | — |

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

### 2026-09-29 23:10 IST · Plan · Orchestrator · HANDOFF
- Did: Recorded round-2 answers (R1–R4) in plan §0. Ran `git init` and made the first commit on `main`. Created worktrees `~/work/postctl-wt/ws0-scaffold` and `~/work/postctl-wt/ws1b-postman-local`. Launched WS0 (Sonnet 5.5) and WS1b (GPT-5.6 Sol).
- Files: `docs/plan.md`, `docs/briefs/WS0-*.md`, `docs/briefs/WS1b-*.md`, `.gitignore`, `go.mod`
- Next: When Wave 1 finishes, review and merge `ws/0-scaffold` first, then write the WS1/WS2/WS5 briefs against the real `model` API and launch Wave 2.

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

### 2026-09-29 23:46 IST · WS1b · GPT-5.6 Sol · PROGRESS
- Branch/worktree: ws/1b-postman-local
- Did: Implemented active-partition discovery, mandatory private copying, LevelDB metadata scanning, IndexedDB/Blink/V8/Snappy/blob decoding, v3 collection-tree conversion, environment/globals conversion, structural v2.1 validation, staging output, coverage reporting, and the development CLI.
- Files: `internal/importer/postmanlocal/`, `cmd/postman-extract/`, `docs/postman-local-format.md`
- Decisions: Coverage counts requests containing each script kind, not raw script blocks. Partial request records lacking required method/name fields are omitted and counted as unresolved.
- Tests: Synthetic decoder, key-prefix, blob metadata, tree/script, locate, validation, and staging tests pass; private-data test skips unless its fixture directory exists.
- Blockers / questions: None.
- Next: Run the real private staging acceptance check and full build/race/lint suite.

### 2026-09-29 23:49 IST · WS1b · GPT-5.6 Sol · DONE
- Branch/worktree: ws/1b-postman-local
- Did: Ran the extractor on the active Postman partition and wrote validated JSON to the gitignored private staging directory. Final live-cache coverage is 2 collections, 11 folders, 23 emitted requests, 17 requests with pre-request scripts, 0 with test scripts, 3 environments, and 0 live globals records; 6 partial request references were reported unresolved.
- Files: `docs/ledger.md`; real output only under gitignored `testdata/private/staging/`
- Decisions: The six partial requests are not emitted as malformed v2.1 items. The absence of test scripts and globals is reported as cache coverage, not inferred data.
- Tests: `go build ./...` passed; `go test -race ./...` passed; `golangci-lint run ./internal/importer/postmanlocal/... ./cmd/postman-extract/...` passed with 0 issues.
- Blockers / questions: Known cache gaps are documented; use Postman File → Export when uncached resources are required.
- Next: none

### 2026-09-29 23:55 IST · WS1b · Orchestrator · PROGRESS
- Did: Reviewed and merged `ws/1b-postman-local` into `main`. Independently re-checked: build and race tests pass; `testdata/private/` is gitignored and absent from commits; a scan of committed docs and tests found no real URLs, tokens or credentials. Parsed the staged output: 2 valid v2.1 collections, 23 requests, 17 with non-empty pre-request scripts, 3 environments.
- Decisions: The local cache is partial (6 unresolved request references, 0 test scripts). `postctl import postman-local` will wrap this package; File → Export JSON remains the fallback for anything uncached.
- Next: Wire into the `postctl import` CLI once WS0 lands `cmd/postctl`. WS1's importer brief will use `testdata/private/staging/` as its real-data fixture.

### 2026-09-29 23:10 IST · WS0 · Sonnet 5.5 · STARTED
- Branch/worktree: ws/0-scaffold
- Did: Read brief, plan §0, ledger, and reference repo docs/code (design-language, tui-stack, AGENTS, theme, cli, keychain). Confirmed toolchain (Go 1.26.1, task, golangci-lint 2.11.4) and module cache.
- Files: `docs/ledger.md`
- Decisions: none yet.
- Tests: n/a
- Blockers / questions: none
- Next: model types, then store, then scaffold/CLI, theme skeleton, docs.

### 2026-09-29 23:26 IST · WS0 · Sonnet 5.5 · PROGRESS
- Branch/worktree: ws/0-scaffold
- Did: Shipped `internal/model` (Workspace/Collection/Folder/Request/Item tree with ops, URL, KV, Body modes, Auth types incl. OAuth2 grants, Variable/SecretRef/Environment/Globals, Scripts, Settings, Examples, IDs via UUIDv7, Validate) and `internal/store` (Discover/Open/Init, Load, Save/SaveCollection/SaveRequest, env/globals, atomic writes, prune, slug collisions, ordering, SecretStore + MemorySecrets + macOS Keychain via `/usr/bin/security -i`, schema migration hook, local state). Round-trip, golden, secrets-leak, ordering, prune and Keychain tests green (real Keychain test opt-in via `POSTCTL_KEYCHAIN_TESTS=1`, verified once).
- Files: `internal/model/*`, `internal/store/*`, `internal/store/testdata/golden/**`
- Decisions: `Disabled` (not `Enabled`) on KV/Variable/FormPart so zero value = enabled. Nil `*Auth` == inherit. Script `Source` is never in YAML; the store writes sibling `.js` and sets `Script.File` on load. Secret var values and OAuth2 tokens are `yaml:"-"`/cleared and stored under deterministic refs (`env/<id>/<key>`, `oauth2/<ownerID>`, ...). `Load` never reads Keychain; call `Store.Hydrate`. Bulk saves skip fsync (macOS full-fsync of 1000s of files takes seconds); single-file saves fsync.
- Tests: `go test -race ./...` green.
- Blockers / questions: see final report (POSTCTL_HOME semantics, literal credentials in auth fields).
- Next: cmd/postctl, Taskfile, lint config, theme skeleton, docs (model.md, design-language.md, tui-stack.md, AGENTS.md).

### 2026-09-29 23:32 IST · WS0 · Sonnet 5.5 · DONE
- Branch/worktree: ws/0-scaffold
- Did: Added repo scaffold (`cmd/postctl` with --version/--help and stub subcommands exiting 3, `Taskfile.yml`, `.golangci.yml`), `internal/ui/theme.go` token skeleton (dark/light, method, syntax, status classes, production badge, layout breakpoints, glyphs), and docs: `docs/model.md`, `docs/design-language.md` (Clear Signal ported for postctl), `docs/tui-stack.md`, `AGENTS.md`. Fixed lint (godoc enforced on exported identifiers).
- Files: `cmd/postctl/*`, `Taskfile.yml`, `.golangci.yml`, `AGENTS.md`, `docs/{model,design-language,tui-stack}.md`, `internal/ui/*`, `internal/model/*`, `internal/store/*`, `.gitignore`, `go.mod`, `go.sum`
- Decisions: Module deps are only `go.yaml.in/yaml/v3` and `charm.land/lipgloss/v2`. Keychain via `/usr/bin/security -i` over stdin (go-keyring rejected: secrets on argv). Load of 1000 requests ~70 ms. Plan §0 Q10 followed (no send confirmation) even though plan §8 still mentions one.
- Tests: `task build test lint` green; `go test -race ./...` green; `POSTCTL_KEYCHAIN_TESTS=1` run verified against the real Keychain.
- Blockers / questions: (1) `POSTCTL_HOME` overrides only the global fallback dir, not project discovery: OK? (2) Literal credentials in auth fields are written to YAML as-is; should the store auto-move non-`{var}` literals to Keychain? (3) Load is strict (a malformed file fails the whole load); want a lenient mode that reports per-file problems? (4) plan §8 line on prod confirmation contradicts §0 Q10; suggest updating.
- Next: merge `ws/0-scaffold`; WS1/WS2/WS5 can start against `internal/model`, `internal/store`, `internal/ui`.

### 2026-09-30 00:10 IST · WS0 · Orchestrator · PROGRESS
- Did: Reviewed and merged `ws/0-scaffold`. Resolved `go.mod`/`go.sum`/ledger conflicts with the WS1b merge and ran `go mod tidy`. Added missing godoc to WS1b code that the new revive rule flagged. On `main`: build, race tests and lint are green.
- Decisions: Recorded D1–D4 (answers to WS0's open questions) and C1 in plan §0. Fixed the plan §8 production-confirmation inconsistency (badge only). Committed the shared contracts `internal/httpx/types.go` and `internal/vars/scope.go` so WS2, WS3 and WS4 can build in parallel. Split OAuth2 into WS2b.
- Next: Launch Wave 2: WS0.1 (resume), WS1, WS2, WS2b, WS3, WS5.

### 2026-09-30 00:10 IST · Wave 2 · Orchestrator · HANDOFF
- Did: Wrote briefs `docs/briefs/WS1-*.md`, `WS2-*.md`, `WS2b-*.md`, `WS3-*.md`, `WS5-*.md`; created a worktree per workstream from `main`.
- Next: Merge order as agents finish: WS0.1 → WS2 → WS2b → WS3 → WS1 → WS5 (roughly, so contracts settle first). Then write the WS4 engine brief against the real WS2/WS3 APIs.

### 2026-09-29 23:41 IST · WS5 · Sonnet 5.5 · STARTED
- Branch/worktree: ws/5-tui-shell
- Did: Read brief, design language, tui-stack, AGENTS, plan §0/§3/§8, `internal/ui/theme.go`, model/store API and the prod-console-tui reference (app.go, keys.go, components.go).
- Files: `docs/ledger.md`
- Decisions: Charm versions pinned to prod-console-tui (bubbletea v2.0.9, bubbles v2.2.1, lipgloss v2.0.5, x/ansi v0.11.7). `store.LoadLenient` not yet available on this branch: the TUI codes against a small `Loader` interface with a `Load`-based fallback adapter.
- Tests: n/a
- Blockers / questions: none
- Next: components in `internal/ui`, then `internal/tui` (root, layout, tree, env switcher, palette, help, first run, watcher), then `cmd/postctl` launch.

### 2026-09-30 00:55 IST · WS5 · Sonnet 5.5 · PROGRESS
- Branch/worktree: ws/5-tui-shell
- Did: `internal/ui` components with 80/140-column snapshots (unicode, NO_COLOR, ASCII). `internal/tui` shell: three/two/single-pane layouts, focus cycling, `Pane` and action-registry API, collection tree (fold, filter, new/rename/duplicate/move/reorder/delete with async coalescing saves), environment switcher, palette, help, first-run guide, fsnotify watcher with debounced reload that keeps selection, accessible/ASCII/NO_COLOR modes.
- Files: `internal/ui/*`, `internal/tui/**`, `cmd/postctl/main.go`, `docs/tui-stack.md`
- Decisions: see DONE.
- Tests: shell, tree, flow, watcher, golden (11 scenes x 3 sizes, 5 modes x 2 sizes) tests green.
- Next: `cmd/postctl` launch, demo fixture, lint, manual check at 80x24 and 200x60.

### 2026-09-30 01:20 IST · WS5 · Sonnet 5.5 · DONE
- Branch/worktree: ws/5-tui-shell
- Did: Shipped the TUI shell and shared UI components, and `postctl` with no args (plus `--workspace`, `--accessible`, `--ascii`, `NO_COLOR`, `POSTCTL_WORKSPACE`, `POSTCTL_ACCESSIBLE`, `POSTCTL_ASCII`) opens it. `go run ./cmd/postctl --workspace internal/tui/testdata/demo` opens a demo workspace. Checked by hand through a pty at 80x24 and 200x60, dark, NO_COLOR + ASCII, and accessible.
- Files: `internal/ui/{text,header,badges,kvtable,modal,states}.go` (+ additive `theme.go` glyphs `RuleActive`, `VRule`, `Mask`, `Dirty`), `internal/tui/*`, `internal/tui/tuitest/demo.go`, `internal/tui/testdata/{golden,demo}`, `cmd/postctl/main.go` (no-args launch and global flags only), `docs/tui-stack.md` (fsnotify).
- Decisions: (1) WS6 plugs in through `Options.NewRequestPane/NewResponsePane` (`PaneFactory`) and the shared `Registry`; panes get `PaneEnv{Theme, Actions, Backend}`. (2) `Backend` adapts `*store.Store`; it gained `SaveRequest`/`SaveEnvironment` for WS6. (3) Edits change the in-memory workspace at once; a save queue snapshots with a reflect `deepCopy` (until `Clone` lands), runs one save at a time and coalesces repeated saves of one collection. (4) `LoadLenient` is probed by type assertion, falling back to `Load`. Broken files show as error rows (D3). (5) Watcher events during and 900 ms after our own saves are ignored; Ctrl-R and quit wait for pending saves. (6) Fold state is not persisted (`store.State` holds only the active environment). (7) Extras: Alt-Up/Down reorder; first-run import rows copy their command to the clipboard. (8) Accessible mode forces the single-pane layout, no alt screen, no color, ASCII; bold is still used.
- Design checklist: (a) First-time user sees the active environment (header, `env  none`) and the primary action (`Enter Create a request`) at once: pass. (b) Every waiting/error state names what happened, what is intact and what to do (load error, problem row, save failure, reload failure): pass. (c) Whole flow works without mouse, color or Unicode (NO_COLOR/ASCII/accessible goldens): pass. (d) Selection uses a marker plus style; method names and status are text: pass. (e) Readable at 80x24 and reflows at 100/140 (goldens at 80x24, 120x40, 200x60 and breakpoint tests): pass. (f) Secrets absent from every state: env preview masks `••••`/`****`, the test asserts no secret value in the frame, and the workspace is saved unhydrated so the Keychain is never read: pass. (g) Tree reserves method and name width before counts and long path text: pass. (h) Borders only for the modal and pane separators; overlays use a rule, not a box: pass.
- Tests: `go build ./... && go test -race ./... && golangci-lint run` green. Cold start to first frame with 1,000 requests: 44 ms in the harness, 2.9 ms of that in the app (benchmark); process start to first output about 45 ms in a pty.
- Blockers / questions: (1) Fold state persistence needs a field in `store.State`. (2) kqueue uses a descriptor per watched directory; the polling fallback covers huge trees but is untested at that scale. (3) WS6: which of `Ctrl-Enter` and `Ctrl-S` sends? Both are reserved in the key map.
- Next: merge `ws/5-tui-shell` after WS0.1 (swap `deepCopy` for `Clone`, drop the `LoadLenient` shim); then WS6 implements the Request and Response panes.
