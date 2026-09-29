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
| WS0 scaffold/model/store | Sonnet 5.5 (high) | Done, merged to main | ws/0-scaffold | 2026-09-29 23:40 IST |
| WS1 Postman importer | GPT-5.6 Sol (medium) | Done, merged to main | ws/1-importer | 2026-09-29 23:55 IST |
| WS1b local Postman extractor | GPT-5.6 Sol (medium) | Done, merged to main | ws/1b-postman-local | 2026-09-29 23:55 IST |
| WS2 vars + httpx | GPT-5.6 Sol (medium) | Done, merged to main | ws/2-vars-httpx | 2026-09-29 23:55 IST |
| WS2b OAuth2 | Grok 4.7 (high, fast) | Done, merged to main | ws/2b-oauth2 | 2026-09-29 23:58 IST |
| WS0.1 lenient load + Clone | Sonnet 5.5 (resumed WS0) | Done, merged to main | ws/0-scaffold | 2026-09-29 23:45 IST |
| WS3 script runtime | Opus 5 Thinking (high) | Done, merged to main | ws/3-script | 2026-09-30 01:55 IST |
| WS4 engine/runner | Sonnet 5.5 (high) | Done, merged to main | ws/4-engine | 2026-09-30 02:50 IST |
| WS5 UI components + TUI shell | Sonnet 5.5 (high) | Done, merged to main | ws/5-tui-shell | 2026-09-30 00:15 IST |
| WS6a request editor + send wiring | Sonnet 5.5 (high) | Done, merged to main | ws/6a-request-editor | 2026-09-30 02:15 IST |
| WS6b response viewer | Composer 2.5 (fast) | Done, merged to main | ws/6b-response-viewer | 2026-09-30 01:30 IST |
| WS7 CLI | — | Not started | — | — |
| WS8 docs/release | — | Not started | — | — |
| WS9 WebSocket requests | — | Not started (Wave 5; gRPC deferred to v1.1) | — | — |
| WS6c runner + history TUI | Composer 2.5 (fast) | Done (branch) | ws/6c-runner-history | 2026-09-30 03:10 IST |

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

### 2026-09-29 23:35 IST · WS0 · Orchestrator · PROGRESS
- Did: Reviewed and merged `ws/0-scaffold`. Resolved `go.mod`/`go.sum`/ledger conflicts with the WS1b merge and ran `go mod tidy`. Added missing godoc to WS1b code that the new revive rule flagged. On `main`: build, race tests and lint are green.
- Decisions: Recorded D1–D4 (answers to WS0's open questions) and C1 in plan §0. Fixed the plan §8 production-confirmation inconsistency (badge only). Committed the shared contracts `internal/httpx/types.go` and `internal/vars/scope.go` so WS2, WS3 and WS4 can build in parallel. Split OAuth2 into WS2b.
- Next: Launch Wave 2: WS0.1 (resume), WS1, WS2, WS2b, WS3, WS5.

### 2026-09-29 23:35 IST · Wave 2 · Orchestrator · HANDOFF
- Did: Wrote briefs `docs/briefs/WS1-*.md`, `WS2-*.md`, `WS2b-*.md`, `WS3-*.md`, `WS5-*.md`; created a worktree per workstream from `main`.
- Next: Merge order as agents finish: WS0.1 → WS2 → WS2b → WS3 → WS1 → WS5 (roughly, so contracts settle first). Then write the WS4 engine brief against the real WS2/WS3 APIs.

### 2026-09-29 23:38 IST · WS0.1 · Sonnet 5.5 · STARTED
- Branch/worktree: ws/0-scaffold
- Did: Read plan §0 D1–D4. Scope: `store.LoadLenient`, deep `Clone`/`CloneFresh` on model types, `--workspace`/`POSTCTL_WORKSPACE` in `store.Discover`, docs/model.md.
- Files: `docs/ledger.md`
- Decisions: none yet.
- Tests: n/a
- Blockers / questions: none
- Next: Clone, LoadLenient, Discover options, docs.

### 2026-09-29 23:43 IST · WS0.1 · Sonnet 5.5 · DONE
- Branch/worktree: ws/0-scaffold
- Did: (D3) `store.LoadLenient() (*model.Workspace, []*FileError, error)`: broken request/folder/collection/environment/globals files and requests with a missing script file are skipped and reported; `Load` stays strict. (D4) Deep `Clone()` on `Request`, `Folder`, `Collection`, `Environment`, `Globals`, `Auth`, `OAuth2Config`, `Body`, `URL`, `Scripts`, `Script`, `RequestSettings`, `Example`, plus `CloneItem`; `CloneFresh()` on `Request`, `Folder`, `Collection`, `Environment` and `CloneItemFresh` (new IDs recursively, cleared `SecretRef`/`Token.Ref`/`Script.File`, in-memory secret values kept). (D1) `store.Discover(start, opts ...DiscoverOption)` with `WithWorkspace(dir)`, `$POSTCTL_WORKSPACE` (`EnvWorkspace`), new `SourceFlag`/`SourceEnv`; precedence flag > env > walk up > global. `cmd/postctl` untouched. Updated `docs/model.md`.
- Files: `internal/model/clone.go`, `internal/model/clone_test.go`, `internal/store/store.go`, `internal/store/discover.go`, `internal/store/save.go`, `internal/store/store_test.go`, `docs/model.md`, `docs/ledger.md`
- Decisions: Beyond the brief: saving a leniently loaded workspace must not destroy the files it skipped, so `SaveCollection`/`SaveRequest`/`Save` never delete a stale file that fails to load (or whose script is missing, or that sits under a broken folder.yaml), nor a collection/environment file that does not parse; explicit `DeleteCollection`/`DeleteEnvironment` still delete everything. Clone tests are reflection-driven (fill every field, no-alias walk, scribble the clone, original must equal a twin), so a future model field that is not cloned deeply fails them; that test already caught a missed `URL.PathVars` secret ref in `CloneFresh` during development. `Discover` stays source-compatible for existing callers (variadic options).
- Tests: `go build ./... && go test -race ./... && golangci-lint run` green.
- Blockers / questions: none.
- Next: merge `ws/0-scaffold`. WS5 can call `store.LoadLenient`; the CLI/WS1/WS5 wire `--workspace` via `store.WithWorkspace`.

### 2026-09-29 23:45 IST · WS0.1 · Orchestrator · PROGRESS
- Did: Verified that `ws/0-scaffold` (WS0.1) touched only model/store/docs, then merged it into `main` (5c590dd). Build, race tests and lint are green.
- Decisions: Accepted WS0.1's unrequested safeguard: saves never delete files that failed a lenient load. WS2 and WS5 branched before this merge. When they merge, swap WS2's local copy helper for `Request.Clone` and WS5's `Load` adapter for `LoadLenient`, and wire `store.WithWorkspace` into `--workspace`.
- Next: Keep waiting on WS1, WS2, WS2b, WS3 and WS5.

### 2026-09-29 23:38 IST · WS1 · GPT-5.6 Sol · STARTED
- Branch/worktree: ws/1-importer
- Did: Read the authoritative WS1 brief, binding plan decisions, ledger process, agent instructions, and the model/storage contract.
- Files: `docs/ledger.md`
- Decisions: Import conversion will preserve unknown Postman fields in model extras, move configured literal credentials through secret collection variables by default, and restrict real-data diagnostics to names and counts.
- Tests: Not run yet.
- Blockers / questions: None.
- Next: Inspect model/store/local-extractor APIs and CLI scaffold, then implement the importer from synthetic fixtures.

### 2026-09-29 23:44 IST · WS1 · GPT-5.6 Sol · PROGRESS
- Branch/worktree: ws/1-importer
- Did: Implemented v2.0/v2.1 collection, environment, and globals conversion; full request/body/auth/script/example/settings mapping; literal-secret extraction; script compatibility lint; stable-ID re-import with local-only item retention; privacy-safe text/JSON reports; synthetic fixtures and golden workspace coverage.
- Files: `internal/importer/postman/`, `docs/ledger.md`
- Decisions: When enabled and disabled Postman events share one script kind, enabled events form the runnable model script and disabled events are preserved in `Extra`; all-disabled event groups remain a disabled model script.
- Tests: Importer and CLI package tests pass under the race detector; package lint findings were fixed.
- Blockers / questions: None.
- Next: Finish CLI/local-cache integration checks, exercise private staging if present, run the full repository build/race/lint suite, and harden any failures.

### 2026-09-29 23:47 IST · WS1 · GPT-5.6 Sol · DONE
- Branch/worktree: ws/1-importer
- Did: Completed exported-file and local-cache import paths, workspace discovery/creation, global and command-level workspace flags, dry-run re-import reporting, Keychain-backed secret migration, privacy-safe text/JSON output, and local-cache coverage warnings. The private staging acceptance case imports 2 collections, 23 requests, 17 pre-request scripts, and 3 environments, then reloads successfully.
- Files: `internal/importer/postman/`, `cmd/postctl/{main.go,main_test.go,import.go,import_test.go}`, `docs/model.md`, `docs/ledger.md`
- Decisions: Local-only workspace items are retained on re-import; source items moved between folders are replaced once by stable ID. Reports expose names, counts, compatibility categories, and cache gaps only.
- Tests: `go build ./... && go test -race ./... && golangci-lint run` passed; `postctl import postman-local --dry-run --json` passed with expected privacy-safe coverage counts.
- Blockers / questions: None.
- Next: none

### 2026-09-29 23:55 IST · WS1 · Orchestrator · PROGRESS
- Did: Verified `ws/1-importer`. It stayed in scope; race tests and lint are green; no real data in committed fixtures. `TestPrivateStaging` ran (not skipped) and passed. A `postman-local --dry-run` against the real cache wrote nothing and reported 2 collections, 11 folders, 23 requests, 17 pre-request scripts, 3 environments, 0 script-lint warnings, and 1 warning for the 6 unresolved cache references. Merged into `main`.
- Decisions: Did not run a non-dry import into a scratch workspace, to avoid orphaned Keychain entries. Secret migration is covered by WS1's synthetic tests. WS1's deviation is accepted: disabled Postman events are kept in `Extra`, and only enabled scripts run.
- Next: Merge order continues with WS2, WS2b, WS3, WS5 as they finish.

### 2026-09-30 00:10 IST · WS2 · GPT-5.6 Sol · STARTED
- Branch/worktree: ws/2-vars-httpx
- Did: Read the WS2 brief, binding plan decisions, ledger process, repository instructions, model contract, shared vars/httpx contracts, and relevant model/store APIs.
- Files: `docs/ledger.md`
- Decisions: Contract files will be extended additively only. Folder variables are excluded from the collection variable layer per the Postman v2.1 contract.
- Tests: Not run yet.
- Blockers / questions: None.
- Next: Implement and test `internal/vars`, then implement and test `internal/httpx`.

### 2026-09-30 00:35 IST · WS2 · GPT-5.6 Sol · PROGRESS
- Branch/worktree: ws/2-vars-httpx
- Did: Implemented ordered variable scopes, nested/cycle-safe replacement, injectable dynamic-variable generation, request deep-copy/resolution with inherited auth flattening, and model persistence application that preserves secret metadata.
- Files: `internal/vars/*`, `docs/ledger.md`
- Decisions: Dynamic variables are generated per occurrence. Folder variables are not loaded into the collection scope. New persisted variables use the default type while writes to existing secret variables preserve their type and Keychain reference.
- Tests: `go test -race ./internal/vars` passed; `golangci-lint run ./internal/vars/...` passed with 0 issues.
- Blockers / questions: None.
- Next: Implement HTTP request building, transport, auth, cookies, curl generation, and protocol tests.

### 2026-09-30 01:20 IST · WS2 · GPT-5.6 Sol · PROGRESS
- Branch/worktree: ws/2-vars-httpx
- Did: Implemented the HTTP sender with all body modes, streaming binary files, defaults and disabled-header handling, redirects, TLS/HTTP2/timeouts, trace timings, compression, body caps, wire-size accounting, persistent public-suffix-aware cookies, copy-as-curl, and bearer/basic/API-key/Digest/OAuth2/SigV4/Hawk auth. NTLM returns an explicit unsupported error.
- Files: `internal/httpx/*`, `internal/vars/dynamic.go`, `internal/httpx/types.go`, `go.mod`, `go.sum`, `docs/tui-stack.md`, `docs/ledger.md`
- Decisions: Added pure-Go Brotli and public-suffix dependencies as required by the brief. Binary request files stream from disk; the final sent-body snapshot is captured while the transport reads it. Digest supports MD5/MD5-sess/SHA-256/SHA-256-sess with `auth` and `auth-int`.
- Tests: Package race tests and focused lint pass; tests cover every body mode, static/OAuth2/Digest/SigV4/Hawk/unsupported NTLM auth behavior, redirects, compression, truncation, timeout errors, TLS HTTP/2, cookie persistence and management, defaults, disabled headers, and curl escaping.
- Blockers / questions: None.
- Next: Run the full repository build/race/lint acceptance suite, fix integration findings, and record DONE.

### 2026-09-30 01:30 IST · WS2 · GPT-5.6 Sol · DONE
- Branch/worktree: ws/2-vars-httpx
- Did: Completed `internal/vars` and `internal/httpx` end to end, including tests and dependency documentation. Exported variable stores/resolution/application and an HTTP client, persistent cookie jar, auth/token integration, and curl generation.
- Files: `internal/vars/*`, `internal/httpx/*`, `go.mod`, `go.sum`, `docs/tui-stack.md`, `docs/ledger.md`
- Decisions: Additive contract changes only: `internal/vars/scope.go` adds `RandomSource`; `internal/httpx/types.go` adds `TokenSource`, `Options.OAuth2`, and `Options.CookieJar`. Dependencies added: `github.com/andybalholm/brotli` for Brotli decoding and `golang.org/x/net/publicsuffix` for cookie policy.
- Tests: `go build ./...` passed; `go test -race ./...` passed; `golangci-lint run` passed with 0 issues.
- Blockers / questions: No blockers. Allowed deviation: NTLM returns a clear `ErrUnsupportedAuth`. `net/http` preserves duplicate response header values but not original wire order/casing, so `Response.Headers` is deterministic rather than wire-ordered and header wire size is reconstructed.
- Next: none

### 2026-09-29 23:58 IST · WS2 · Orchestrator · PROGRESS
- Did: Verified `ws/2-vars-httpx`. It stayed in scope (plus a `docs/tui-stack.md` note for the brotli and publicsuffix dependencies). Contract changes only add things: `vars.RandomSource`, `httpx.TokenSource`, `Options.OAuth2`, `Options.CookieJar`. Race tests and lint are green. Merged into `main`, then replaced WS2's local request/auth copy helpers with `model.Request.Clone` / `Auth.Clone` from WS0.1; tests are still green.
- Decisions: Accepted two deviations. NTLM returns `ErrUnsupportedAuth`. Response header order and casing are normalized by `net/http`, not kept as sent on the wire.
- Next: WS2b implements `httpx.TokenSource`; WS3 is coding against these contracts. The WS4 engine brief can now be written against real APIs.

### 2026-09-29 23:41 IST · WS2b · Grok 4.7 · STARTED
- Branch/worktree: ws/2b-oauth2
- Did: Read the WS2b brief, plan §0 (R2, D2), model rule 6, `OAuth2Config` / `OAuth2Token`, `SecretStore` / `HydrateAuth`, the httpx contract, and the prod-console-tui loopback login. Starting `internal/oauth2` only.
- Files: `docs/ledger.md`
- Decisions: Standard library only (`golang.org/x/oauth2` would hide PKCE, loopback, implicit fragment delivery, and the no-auto-refresh rule, and a new dependency cannot be recorded in `docs/tui-stack.md` from this workstream). Token secrets use the store's JSON shape (`access`, `refresh`, `id`) so `HydrateAuth` can read them. `Source.Token` matches the forthcoming `httpx.TokenSource` signature without importing httpx.
- Tests: Not run yet.
- Blockers / questions: None.
- Next: Implement grant fetches, loopback, and the cached token source.

### 2026-09-29 23:55 IST · WS2b · Grok 4.7 · DONE
- Branch/worktree: ws/2b-oauth2
- Did: Shipped `internal/oauth2`. `Fetch` runs client credentials, authorization code, authorization code with PKCE (S256 or plain), password, and implicit, plus an on-demand refresh. Browser grants listen on 127.0.0.1, honor a localhost redirect's port and path, and otherwise use an ephemeral loopback URI reported through `FetchOptions.OnNote`. The authorization URL is passed to `OnAuthURL` before the browser opens. `Source.Token` returns a cached unexpired token or `ErrNoToken` and does not fetch. `Source.Save` writes access, refresh, and ID token strings as the store's JSON secret (`access`, `refresh`, `id`).
- Files: `internal/oauth2/`, `docs/ledger.md`
- Decisions: Standard library only. `Source.Token` matches the forthcoming `httpx.TokenSource` signature and does not import httpx. The loopback note is `FetchOptions.OnNote` because `model.OAuth2Token` has no note field. `Save` takes an owner id so an empty `Token.Ref` becomes `oauth2/<owner>`. Flat `OAuth2Config.Extra` entries are sent on authorize and token requests; `authRequestParams`, `tokenRequestParams`, and `refreshRequestParams` stay on that step. Sensitive extra keys are dropped. Scope is not repeated on the authorization-code token exchange. `client_id` is always in the token body; the secret is Basic or the body per `ClientAuth`.
- Tests: `go build ./...`, `go test -race ./...`, and `golangci-lint run` passed (0 issues). Implementation commit `bf7eb52`.
- Blockers / questions: None.
- Next: none

### 2026-09-29 23:59 IST · WS2b · Orchestrator · PROGRESS
- Did: Verified `ws/2b-oauth2`. It touched only `internal/oauth2/` and the ledger; standard library only; no logging of tokens. Merged into `main` and added a compile-time assertion that `oauth2.Source` implements `httpx.TokenSource`. Build, race tests and lint are green.
- Decisions: Accepted deviations. The loopback note comes through `FetchOptions.OnNote`. `Save` takes an owner ID (the ref is `oauth2/<owner>`). Non-loopback or https redirect URIs bind an ephemeral `127.0.0.1` listener, as Postman does. The TUI sequence for auth code is documented in WS2b's DONE entry; WS6 will implement "Get new token" against it.
- Next: WS4 engine wires `oauth2.Source` into `httpx.Options.OAuth2`.

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

### 2026-09-30 01:05 IST · WS6b · Composer 2.5 (fast) · DONE
- Branch/worktree: ws/6b-response-viewer
- Did: Shipped `internal/tui/response` Response pane (summary chip, six tabs, send/skip/error states, JSON pretty/fold/search/path filter, raw/hex virtualization, Chroma→theme syntax tokens, timeline with masked secrets). Wired `NewResponsePane` in `cmd/postctl`. Added `RetrySendMsg` to `send.go`. Demo fixture via `POSTCTL_DEMO_RESPONSE=1`.
- Files: `internal/tui/response/**`, `cmd/postctl/main.go`, `internal/tui/send.go`, `go.mod`, `go.sum`, `docs/ledger.md`
- Decisions: Per-request results keyed by ID survive selection and workspace reload pruning; save-body uses toast stub until shell path prompt; pager via temp file + `$PAGER`.
- Design checklist: status code+reason text with status-class tokens (pass); NO_COLOR/ASCII goldens (pass); error-state order on transport failures (pass); secrets masked in timeline/cookies (pass); 80×24 tab collapse selector (pass); spinner always paired with step text (pass).
- Tests: `go build ./... && go test -race ./... && golangci-lint run` green. BenchmarkLargeBodyFirstRender ~4.1 ms/op on ~10 MB raw body (visible window only).
- Blockers / questions: Shell must handle `RetrySendMsg` and real send messages (WS6a/WS4). Copy-at-path and save-to-file prompt deferred to palette/shell integration.
- Next: none

### 2026-09-30 00:25 IST · WS6b · Composer 2.5 (fast) · STARTED
- Branch/worktree: ws/6b-response-viewer
- Did: Read brief, design-language, send contract, pane API, and ui components. Added chroma and gjson dependencies.
- Files: `docs/ledger.md`, `go.mod`, `go.sum`
- Decisions: Implement `internal/tui/response/` as the sole pane owner; wire `NewResponsePane` in `cmd/postctl`; demo via `POSTCTL_DEMO_RESPONSE=1`.
- Tests: Not run yet.
- Blockers / questions: WS6a send wiring may land later; pane consumes send messages directly.
- Next: Core pane, body virtualizer, tabs, tests and goldens.

### 2026-09-30 00:20 IST · WS5 · Orchestrator · PROGRESS
- Did: Verified `ws/5-tui-shell` (in scope; golden renders reviewed by hand, e.g. the delete dialog at 200×60 defaults to Cancel and names the folder and request count). Merged into `main`, resolving `go.mod`/`go.sum`/ledger conflicts and a duplicate global `--workspace` flag between WS1 and WS5 in `cmd/postctl/main.go`. Build, race tests and lint are green.
- Decisions: D5 (send keys) and D6 (persist fold state) answer WS5's questions. WS5's `deepCopy`/`LoadLenient`-probe stand-ins still work (the probe now finds the real method); their cleanup is assigned to WS6a. Added C2: `internal/tui/send.go` send contract.
- Next: Launch WS6a (Sonnet 5.5) and WS6b (Composer 2.5) in parallel. WS4 (engine) starts when WS3 merges and will implement `tui.Sender`.

### 2026-09-30 01:30 IST · Orchestrator · PROGRESS
- Did: Found WS3 and WS6a had stopped mid-run. WS3 had committed up to a17f1ca with no DONE entry; WS6a had only its STARTED entry and no implementation. Resumed both agents with their context and asked them to commit after each milestone. Verified WS6b: in scope, race tests and lint green, 10 MB JSON first render 4.8 ms (target < 50 ms), golden renders reviewed by hand (NO_COLOR JSON body, ASCII timeline with Authorization masked). Merged WS6b into `main`.
- Decisions: Accepted WS6b's additive contract `RetrySendMsg` in `internal/tui/send.go`. WS6a's shell send wiring must handle it; check this when merging WS6a.
- Next: Wait for WS3 and WS6a. WS4 (engine, implements `tui.Sender`) starts when WS3 merges.

### 2026-09-30 00:18 IST · WS3 · Opus 5 Thinking · STARTED
- Branch/worktree: ws/3-script
- Did: Read the brief, plan §0 (Q4/R3/C1), §4.1, §7, `AGENTS.md`, `docs/model.md`, `internal/model`, and the C1 contracts `internal/vars/scope.go` and `internal/httpx/types.go`. Confirmed the toolchain (Go 1.26.1, node 22 via nvm for development-time bundle fetching only) and added `github.com/dop251/goja` + `github.com/dop251/goja_nodejs` to `go.mod`.
- Files: `go.mod`, `go.sum`, `docs/ledger.md`
- Decisions: Engine is goja + goja_nodejs (pure Go, no cgo) per plan §4. npm libraries will be vendored as pinned, minified, ES5 CommonJS bundles under `internal/script/assets/` and embedded; node/npm is a development-time-only dependency.
- Tests: none yet.
- Blockers / questions: none.
- Next: Build the bundle pipeline and the core `pm.*` surface against in-package fakes for `vars.Store` and `httpx.Sender`.

### 2026-09-30 00:06 IST · WS3 · Opus 5 Thinking · PROGRESS
- Branch/worktree: ws/3-script
- Did: Built the development-time bundle pipeline (browserify + terser, ES5) and embedded ten pinned npm libraries under `internal/script/assets/bundles/`; each one was checked to actually load and run in goja. Implemented the core `pm.*` surface in JavaScript against a narrow Go bridge: `PropertyList` and the property types, `Url`, `Request`, `Response`, `VariableScope`, `pm.test`/`pm.expect` on real chai + chai-postman, and the `Run`/`Input`/`Result` API.
- Files: `internal/script/{script,convert,programs,bridge,run}.go`, `internal/script/assets/{package.json,build.js,assets.go,bundles/,sandbox/}`
- Decisions: (1) The `pm.*` objects are hand-written against postman-collection's source rather than built on the `postman-collection` bundle, because that bundle costs ~26 ms to evaluate per VM against a 5 ms budget for a whole script; the bundle stays requirable by name. (2) `crypto` and `console` globals are polyfilled before any bundle loads — uuid and postman-collection need `crypto.getRandomValues`, ajv needs `console` — with randomness from Go's `crypto/rand`. (3) crypto-js is pinned to 4.2.0 rather than Postman's 3.3.0, which carries CVE-2023-46233; the only visible difference is the PBKDF2 defaults. (4) Go↔JS passes Postman v2.1 definitions as JSON strings, so the wire format is the one both sides already understand.
- Tests: Unit tests for console capture, variable scopes, pm.test and request mutation; a load-and-run test per bundle.
- Blockers / questions: none.
- Next: `pm.sendRequest`, timers and the timeout/interrupt path.

### 2026-09-30 00:16 IST · WS3 · Opus 5 Thinking · PROGRESS
- Branch/worktree: ws/3-script
- Did: Added async `pm.sendRequest` in both callback and promise form, the event loop with `setTimeout`/`setInterval`, the timeout and interrupt path, the cookie jar, the tier-3 legacy globals, and error mapping from goja positions back to `Script.File` and line. Added benchmarks.
- Files: `internal/script/{run,bridge}.go`, `internal/script/assets/sandbox/{70-cookies,80-pm,90-legacy,99-boot}.js`, `internal/script/*_test.go`
- Decisions: (1) `pm.sendRequest` creates its keep-alive timer in JavaScript, not Go: `eventloop.SetTimeout` increments the job count asynchronously, which lets the loop exit before an in-flight send lands. (2) `eventloop.Terminate` leaves the job count inflated, so the finish step talks to the runtime directly instead of scheduling one more loop job. (3) goja reports syntax errors as `file: Line L:C msg`, not `file:line:col`, so positions are parsed from that form.
- Tests: `go test -race ./internal/script` green, including timeout, pending-timer and context-cancellation cases. Warm run of a trivial script 0.47 ms; crypto-js signing 3.4 ms; five chai assertions 3.7 ms — all inside the 5 ms budget.
- Blockers / questions: none.
- Next: The compatibility corpus and the private-data run.

### 2026-09-30 01:45 IST · WS3 · Opus 5 Thinking · PROGRESS
- Branch/worktree: ws/3-script
- Did: Wrote the 46-case compatibility corpus (golden `.js` + `.json` pairs under `internal/script/testdata/corpus/`, regenerable with `-update`) and the private-data test over the extracted collections.
- Files: `internal/script/{corpus,private}_test.go`, `internal/script/testdata/corpus/`, `internal/script/assets/sandbox/{10-util,30-url,60-varscope}.js`, `internal/script/bridge.go`, `internal/script/assets/README.md`, `Taskfile.yml`
- Decisions: Writing the corpus found four fidelity bugs, now fixed: `Url.update` ignored a `{raw: …}` definition so host/path/protocol were empty for every script that read `pm.request.url`; `Url.toString` left `:name` path segments unresolved where postman-collection substitutes them (`toJSON` still emits the unresolved form, since the values travel separately and `model.URL` keeps them apart); `console` did no printf substitution; and `toObject()` crossed into JavaScript as a Go map, so its key order changed between runs.
- Tests: **Private data: 17 of 17 real pre-request scripts ran clean — no ScriptError, no unsupported-API warning.** Across the 17 chains the scripts made 30 variable writes and 17 `pm.sendRequest` calls, so they are doing real work, not exiting early. Counts only; nothing from that data is logged anywhere.
- Blockers / questions: none.
- Next: `docs/scripting.md`, then the full build/test/lint gate.

### 2026-09-30 01:52 IST · WS3 · Opus 5 Thinking · DONE
- Branch/worktree: ws/3-script (not merged, not pushed)
- Did: `internal/script` is complete: the goja sandbox, the T1+T2 `pm.*` surface plus the cheap T3 legacy globals, ten embedded npm bundles, async `pm.sendRequest`, timers and the event loop, timeouts via `vm.Interrupt`, the cookie jar, error mapping to `Script.File` and line, a 46-case compatibility corpus, the private-data test, and `docs/scripting.md`.
- Files: `internal/script/**`, `docs/scripting.md`, `internal/script/assets/README.md`, `Taskfile.yml` (new `assets` task), `go.mod`, `go.sum`
- Decisions: No contract changes were needed — `internal/vars/scope.go` and `internal/httpx/types.go` are untouched, and `internal/model` is untouched. The only edits outside `internal/script/` and `docs/scripting.md` are the additive `task assets` target and the two goja dependencies. WS4 should call `script.Run(ctx, chain, script.Input{…})` once per event and persist `Input.Vars.Changes()` afterwards; `Result.Request` is always a distinct value from `Input.Request`, so the pipeline can diff them.
- Deviations from Postman, all documented in `docs/scripting.md`: crypto-js 4.2.0 instead of 3.3.0 (CVE-2023-46233; only the PBKDF2 defaults differ); `pm.*` objects are postctl's own implementation of postman-collection's semantics, so `pm.request instanceof sdk.Request` is false; query strings are not percent-normalised, which keeps `{{variable}}` intact; `<scope>.values` is a snapshot because postctl holds no variable state of its own; `pm.visualizer`, `pm.vault`, `pm.require` and `pm.execution.runRequest` warn and no-op; `pm.info.collectionName` is an addition.
- Tests: `go build ./...`, `go test -race ./...` and `golangci-lint run` all green. Corpus 46/46. **Private data: 17 of 17 real pre-request scripts run clean, no unsupported-API errors or warnings.** Warm run of a trivial script 0.47 ms, crypto-js signing 3.4 ms, five chai assertions 3.7 ms, against a 5 ms budget.
- Blockers / questions: none.
- Next: none. Ready for the orchestrator to review and merge.

### 2026-09-30 02:00 IST · WS3 · Orchestrator · PROGRESS
- Did: Verified `ws/3-script`. It stayed in scope (plus the `task assets` target and goja dependencies); race tests and lint are green. `TestPrivateCollections` ran (not skipped) and passed: all 17 real pre-request scripts run clean. The committed bundles total 1.8 MB, and `node_modules` is gitignored. Corpus fixtures contain no private data. Merged into `main`.
- Decisions: Accepted deviations. crypto-js is 4.2.0 (the CVE fix) instead of Postman's 3.3.0. `pm.*` is postctl's own implementation (`instanceof sdk.Request` is false). Query strings are not percent-normalized. Found an integration gap: `httpx.CookieJar` doesn't implement `script.CookieAccess` (missing `ClearCookies`); the WS4 brief assigns an adapter.
- Next: Launch WS4 (engine, runner, history, tuiengine adapter). WS7 (CLI send/run/ls/env) follows WS4. The TUI runner screen and history pane will be briefed after WS4 and WS6a merge.

### 2026-09-30 00:16 IST · WS6a · Sonnet 5.5 · STARTED
- Branch/worktree: ws/6a-request-editor
- Did: Read the WS6a brief, design language, tui-stack, AGENTS, plan §0/§8, the WS2b DONE entry, and the shell code (`pane.go`, `app.go`, `backend.go`, `send.go`, `placeholder.go`, `save.go`, `keys.go`).
- Files: `docs/ledger.md`
- Decisions: Send wiring first (shell + `FakeSender`), then the request pane package (URL bar, tabs, autosave, undo), then `$EDITOR`, curl import/copy, OAuth2, env editor, cookie manager. Extra work in `internal/tui` is confined to send wiring, backend cleanup, fold persistence and overlay hosting.
- Tests: n/a
- Blockers / questions: none yet
- Next: shell send wiring and backend cleanup.

### 2026-09-30 01:50 IST · WS6a · Sonnet 5.5 · PROGRESS
- Branch/worktree: ws/6a-request-editor
- Did: Restarted after an interrupted run and committed per milestone. (1) Stand-in cleanup (`Clone`, direct `LoadLenient`, `WithWorkspace`) and `State.Folded` fold persistence. (2) Shell send wiring: `Options.Sender`, one send in flight per request, cancel, `RetrySendMsg`, a serialized `SaveMsg` queue with coalescing, screen hosting, `Ctrl-E`, and `internal/tui/fakesend`. (3) `internal/tui/request`: URL bar with `{{var}}` highlight and masked resolved preview, seven tabs, key-value tables, autosave (500 ms plus blur, selection change), undo/redo. (4) Body (JSON validate/format), scripts with snippets, settings, `$EDITOR` round trip. (5) Auth: all types, Inherit shows the effective auth and its source, OAuth 2.0 "Get new token". (7) curl copy (masked by default) and paste-to-import.
- Files: `internal/tui/request/*`, `internal/tui/textin`, `internal/tui/fakesend`, `internal/tui/{send,sendrun,save,screens,app,pane,backend,keys,envs,sidebar,tree}.go`, `internal/store/state.go`, `cmd/postctl/main.go`.
- Decisions: see DONE.
- Tests: request-pane tests with a fake clock, all green under `-race`.
- Next: environment editor, cookie manager, wiring, goldens, checklist.

### 2026-09-30 02:15 IST · WS6a · Sonnet 5.5 · DONE
- Branch/worktree: ws/6a-request-editor
- Did: Shipped the Request pane, environment editor, cookie manager and the shell send wiring. `go run ./cmd/postctl --workspace internal/tui/testdata/demo` (run on a copy, in a pty at 80x24 and 140x40): open a request, edit it, send with the fake sender and watch "Running pre-request script", "Waiting for response"; Ctrl-E edits an environment and the change reaches `environments/dev.yaml` within a second; the palette opens Cookies. Environment editor: create, rename, duplicate (secrets copied by value into new Keychain entries, not shared), delete behind a danger dialog that defaults to Cancel, production toggle, variables table (add, edit, toggle, delete, reorder, secret toggle, reveal), Globals and Collection-variable tabs, save through `SaveEnvironment`/`SaveGlobals`/`SaveCollection`/`DeleteEnvironment`. Cookie manager: cookies grouped by domain, detail with masked value, delete, clear domain and clear all behind confirmations.
- Files: `internal/tui/request/*` (+ `testdata/*.golden`: 7 tabs x 3 sizes x dark/NO_COLOR/ASCII, plus 8 states at 80x24 and 200x60), `internal/tui/envedit/*`, `internal/tui/cookies/*`, `internal/tui/fakesend/*`, `internal/tui/textin/*`, `cmd/postctl/wire.go`, `docs/tui-stack.md` (keys). Shell: `send.go` (additive `RetrySendMsg`), `sendrun.go`, `save.go`, `screens.go`, `app.go`, `pane.go`, `backend.go`, `keys.go`, `envs.go`, `sidebar.go`, `tree.go`; `internal/store/state.go` (`Folded`).
- Decisions: (1) `FakeSender` lives in `internal/tui/fakesend`, not `tui`, so it can be used from `cmd` without an import cycle; `cmd/postctl/wire.go` (an `init` that wraps `launchUI`) sets `Sender`, `NewRequestPane`, `NewEnvEditor` and `NewCookieManager` and leaves the `NewResponsePane` line in `main.go` alone. WS4 replaces `newSender()`. (2) A pane may implement `BlurCmd() tea.Cmd`; the shell calls it when focus leaves the Request slot so edits are saved on blur. (3) The single-line input was extracted to `internal/tui/textin` for the request pane and the env editor. (4) Panes edit the live workspace on the Update goroutine; anything slow (curl build, OAuth, Keychain reads, `$EDITOR`) works on snapshots inside a command. Secrets are never in memory unless revealed. (5) Cookie manager and the engine will each open `cookies.json` through `httpx.NewCookieJar`; WS4 should hand the engine's jar to `cookies.Factory` instead, so both share one in-memory jar. (6) No vim keys anywhere (Q11).
- Design checklist: (a) Active environment and the primary action visible at once (header `env`, URL bar `Send`, footer `Ctrl-S Send`, or `Ctrl-Enter Send` with an enhanced keyboard): pass. (b) Waiting and error states say what and what next: sending progress with `Esc Cancel`, OAuth "Waiting for you to sign in" with the link and `c Copy link`, invalid JSON, `Not saved` with retry on the next edit, no secret store for OAuth, empty environments and cookies guides: pass. (c) Whole flow works without mouse, color or Unicode (NO_COLOR and ASCII goldens for every tab; labels and `PROD`/`unresolved` are text): pass. (d) Selection is a marker plus style (`›`, `>` in ASCII), toggles are `[x]`/`[ ]`: pass. (e) 80x24 usable for every tab, screen and dialog; goldens at 80x24, 120x40, 200x60; a test asserts every tab fits its size exactly: pass. (f) Secrets absent: URL preview and auth masked, curl copy masked unless `Y`, env and cookie values masked until revealed, reveal resets on move, tests assert the frame never holds the secret: pass. (g) Method and URL reserve width before `Send` and counts; tab row scrolls with a cue instead of clipping: pass. (h) Borders only for dialogs and the pane separators: pass.
- Tests: `go build ./... && go test -race ./... && golangci-lint run` green (0 issues). New: request pane (78 tests incl. autosave, undo, OAuth, curl, `$EDITOR`), env editor (22), cookie manager (7), shell send/save queue/screen hosting/blur hook, and about 110 goldens.
- Blockers / questions: (1) `Ctrl-Enter` only works where the terminal reports enhanced keys; `Ctrl-S` always sends (D5), and Ctrl-S is also XOFF on terminals with software flow control. (2) `View` of the env editor, cookie manager and textarea adjust scroll offsets, which is idempotent but not strictly pure. (3) The response pane is the placeholder until WS6b merges.
- Next: merge `ws/6a-request-editor` (no overlap with `internal/tui/response`; `cmd/postctl/main.go` only changes the workspace discovery in `openUI` (`store.WithWorkspace`), not the `tui.Options` literal WS6b edits). WS4 implements `tui.Sender` and swaps `newSender()`.

### 2026-09-30 02:15 IST · WS6a · Orchestrator · PROGRESS
- Did: Verified `ws/6a-request-editor`. It stayed in scope, and contract changes only add things (`RetrySendMsg` is declared once and handled in `app.go`). Merged into `main`. Build, race tests and lint are green. Ran a pty smoke test at 140×40 on a copy of the demo workspace: opened "Create charge", pressed `Ctrl-S`, and saw the fake sender's `200 OK` rendered by WS6b's response pane with a highlighted JSON body. With no environment selected, the URL bar showed `! unresolved: baseUrl`.
- Decisions: Accepted WS6a deviations (`fakesend` package, `BlurCmd` hook, `textin`, `wire.go` `init()`). QA follow-up: envedit, cookies and textarea adjust scroll offsets inside `View`; move that into `Update`.
- Next: At WS4 merge, swap `newSender()` for `tuiengine` and share one cookie jar.

### 2026-09-30 02:05 IST · WS4 · Sonnet 5.5 · STARTED
- Branch/worktree: ws/4-engine
- Did: Read the WS4 brief, plan §0/§4/§4.1/§9, model.md, scripting.md, the DONE entries of WS2, WS2b and WS3, and the vars/httpx/oauth2/script/store APIs and `internal/tui/send.go`.
- Files: `docs/ledger.md`
- Decisions: (1) One internal `session` drives both `Send` and the runner, so a run keeps one `vars.MemStore` across requests and iterations and persists var changes incrementally. (2) Only variables are hydrated (on clones); OAuth 2.0 token strings are read lazily by `oauth2.Source` through the token's `Ref`, so auth is never hydrated. (3) Concurrent sends merge through a per-workspace in-memory commit log so a persist replays other sends' changes instead of overwriting them. (4) The cookie adapter is built only on the existing public `httpx.CookieJar` API (`Get`, `Set`, `List`, `Delete`), so httpx is not touched.
- Tests: n/a
- Blockers / questions: `store` has no way to load just `globals.yaml`; the engine reads globals through `LoadLenient` and caches by file stat until the orchestrator adds a narrower call.
- Next: engine send pipeline, cookie adapter, runner, history, tuiengine, docs.

### 2026-09-30 02:20 IST · WS4 · Sonnet 5.5 · PROGRESS
- Branch/worktree: ws/4-engine
- Did: Milestones 1-3 committed. `internal/history` (append-only JSONL, bounded by 1,000 entries / 50 MB with hysteresis compaction, advisory file lock for TUI+CLI sharing, `Redactor`), and `internal/engine` send pipeline (`New`, `Send`, `Outcome`, steps, cookie adapter `NewCookieAccess`, history recording, R3 persistence with a per-workspace commit log so concurrent sends merge). Pipeline tests cover the full env-persist/secret path, chain order, skipRequest, pre-request error, test error, unresolved warning, OAuth2 no-token and stored token, cookies, `pm.sendRequest`, cancellation, timeout, globals.
- Files: `internal/history/*`, `internal/engine/*`
- Decisions: (1) Scripts of a chain run one `script.Run` per level (not one shared VM) so progress can name each level and `skipRequest` stops the chain; the live `vars.Store` carries state across them. (2) A failing test script stops the remaining test levels but keeps response, tests so far and variables; a failing pre-request script stops the send (no history entry). (3) The engine snapshots `Collection`, `Environment` and `Globals` with `Clone()` when a send starts; it saves the collection snapshot for collection-variable writes (see the risk below). (4) Only secrets a script changed are re-written to the SecretStore on persist; the rest are saved by reference to avoid one Keychain write per secret per send. (5) `engine.New` returns `(*Engine, error)` because opening the cookie jar and history can fail.
- Tests: `go test -race ./internal/history ./internal/engine` green so far.
- Blockers / questions: (a) Risk for the orchestrator: `store` has no narrow "save collection variables" call, so a script that writes a collection variable makes the engine call `SaveCollection` with the snapshot taken when the send began; an edit the user made to that collection during the send would be reverted on disk (and the TUI watcher would reload it). Requested: `store.SaveCollectionMeta(ctx, c)` that rewrites only `collection.yaml`. (b) `store` also has no `LoadGlobals`; the engine loads globals via `LoadLenient` and caches by file stat. (c) Hydration reads every secret variable of the env, collection and globals on each send; with the real Keychain (one `security` process per read) an environment with many secrets will add latency. A per-process cache invalidated by `SaveEnvironment` would fix it and belongs in `store`.
- Next: runner, then tuiengine adapter, then `docs/engine.md`.

### 2026-09-30 02:50 IST · WS4 · Sonnet 5.5 · DONE
- Branch/worktree: ws/4-engine (not merged, not pushed)
- Did: Shipped `internal/engine` (send pipeline, R3 persistence with a concurrency-safe commit log, cookie adapter, `pm.sendRequest` sender, Postman-style runner with typed events and summary, CSV/JSON data loaders), `internal/history` (bounded append-only JSONL, redaction) and `internal/tui/tuiengine` (`tui.Sender` adapter), plus `docs/engine.md` (pipeline sequence diagram, runner semantics, history format).
- Files: `internal/engine/**`, `internal/history/**`, `internal/tui/tuiengine/**`, `docs/engine.md`, `docs/ledger.md`. Nothing else was touched: no change to httpx, script, store, model, vars, oauth2, `cmd/postctl` or the rest of `internal/tui`.
- Decisions: see the PROGRESS entry above, plus: (1) `engine.New` returns `(*Engine, error)`. (2) Warnings have no field in `tui.SendResult`, so the adapter puts them first in `Console` as `warn` lines with event `engine`; a `Warnings []string` field on `SendResult` would be cleaner (orchestrator/WS6a call). (3) Extras beyond the brief, all additive: `SendInput.Globals/Locals/IterationCount`, `RunInput.Locals`, `RequestProgress` event, `RunAsync`, `RunSummary.ExitCode()` (plan §9 mapping, for WS7), `LoadDataFile`, `Engine.Cookies()`, `Config.DisableHistory/Defaults/MaxBodyBytes`.
- Deviations from Postman (also in `docs/engine.md`): each script level runs in its own sandbox; a failing test script stops the remaining test levels; `setNextRequest` to an unknown request stops the run with a script error; data rows are reused (modulo) when iterations exceed rows; a skipped request runs no tests and is not recorded in history.
- Tests: `go build ./... && go test -race ./... && golangci-lint run` green (0 issues). Engine integration tests cover the brief's list (full pipeline with env YAML and MemorySecrets persistence, chain order, skipRequest, pre-request error stops, test error continues, unresolved warning, OAuth2 no-token, cookie set then read by `pm.cookies`, `pm.sendRequest` from a pre-request, runner iterations with CSV, `setNextRequest` jumps/loops/stop, bail, cancellation, event order, 24 concurrent sends and a run racing sends without a lost write), and a check that no secret value appears in any history file.
- Blockers / questions for the orchestrator: (a) request `store.SaveCollectionMeta` (rewrite only `collection.yaml`) and `store.LoadGlobals`; until then a collection-variable write saves a start-of-send collection snapshot, which can revert an edit made during the send. (b) Keychain reads per send: add a cache in `store` if latency shows. (c) WS2's cookie jar stores cookie values in plaintext `.postctl/cookies.json` (0600); history redacts them but the jar does not. Policy decision for you. (d) Wiring: `tuiengine.New(eng)` needs an `*engine.Engine` built from the same `*store.Store` the TUI backend uses (`engine.New(engine.Config{Store: st, Version: version})`); the shell should pass a snapshot of the collection or accept that the engine clones it at send start.
- Next: none.

### 2026-09-30 02:22 IST · INT1 · GPT-5.6 Sol · PROGRESS
- Did: Merged WS4 and wired the TUI to a lazily constructed real engine over the TUI backend's store. The engine, OAuth token source and cookie manager share the same secret store and cookie jar; `POSTCTL_FAKE_SENDER=1` retains the demo sender. Implemented plan decisions D7–D10.
- Decisions: Collection-variable persistence now re-reads and rewrites only `collection.yaml`; globals have a narrow loader; cookies remain plaintext local state with mode `0600`; engine warnings have a dedicated TUI field and render as labeled warning lines; secret reads use a process-local invalidating cache.
- Tests: Full build, race tests and lint are green. Built `/tmp/postctl` and ran it through the pty harness at 140×40 against a temporary synthetic workspace and local HTTP server. Selected "List charges", pressed `Ctrl-S`, and the response pane rendered the real server's `404 File not found` status, timing and body.
- Blockers / questions: none.
- Next: final full check and DONE entry.

### 2026-09-30 02:23 IST · INT1 · GPT-5.6 Sol · DONE
- Branch/worktree: `main` in `/Users/shubham/work/postctl`
- Did: Completed the staged WS6a merge, removed its worktree, verified and merged WS4, wired real TUI sends with one shared cookie jar, implemented D7–D10, and passed a real-engine pty smoke test.
- Files: `cmd/postctl/wire.go`, `internal/{engine,httpx,store,tui}/**`, `docs/{engine,plan,ledger}.md`
- Decisions: D7–D10 are locked in plan §0. The fake sender is opt-in with `POSTCTL_FAKE_SENDER=1`; engine construction retries lazily after first-run workspace creation.
- Tests: Final `go build ./... && go test -race ./... && golangci-lint run` passed.
- Blockers / questions: none. Existing QA follow-up remains: move envedit, cookies and textarea scroll-offset adjustment out of `View` and into `Update`.
- Next: WS7 can build CLI send/run against the merged engine.

### 2026-09-30 02:30 IST · WS6c · Composer 2.5 (fast) · STARTED
- Branch/worktree: ws/6c-runner-history
- Did: Read WS6c brief, design language, engine runner/history APIs, and shell screen patterns (envedit/cookies). Created worktree from main.
- Files: `docs/ledger.md`
- Tests: n/a
- Blockers / questions: none
- Next: runner screen, history screen, wire.go, tests and goldens.

### 2026-09-30 02:45 IST · WS6c · Composer 2.5 (fast) · DONE
- Branch/worktree: ws/6c-runner-history
- Did: Shipped `internal/tui/runner` (setup form, live progress via `RunAsync` events, summary with failures, JSON export, request detail) and `internal/tui/history` (day-grouped list, text/status filters, redacted detail with response snapshot rendering, open/re-send/clear/curl). Wired `Options.NewRunner`/`NewHistory`, `CollectionRunner`/`HistoryReader`, tree `R`, global `H`, palette actions, and `lazyEngine.historyLog()` in `cmd/postctl/wire.go`. Added `internal/tui/fakerun`, `tuiengine.Runner`, and `response.Snapshot`.
- Files: `internal/tui/{runner,history,fakerun,runner.go,historyreader.go,response/snapshot.go,tuiengine/runner.go}`, shell `{app,screens,keys,sidebar,layout,pane}.go`, `cmd/postctl/wire.go`, `docs/ledger.md`
- Decisions: (1) Pause skipped — engine has no pause API. (2) Re-run failed in TUI re-runs the full collection until WS7 reporters land; export is JSON-only (no JUnit in tree). (3) `Persist variables` is shown on the setup form; persistence follows engine R3 regardless. (4) History curl uses `httpx.Curl` on already-redacted snapshots.
- Design checklist: (a) Active environment visible in header on both screens: pass. (b) Waiting/cancel/error states explain next step (spinner + step text, cancel confirm, empty history guide): pass. (c) Keyboard-only, NO_COLOR/ASCII goldens for runner setup/summary: pass. (d) Method and status codes are text labels: pass. (e) Readable at 80×24 and 140×40 (runner goldens): pass. (f) Tests assert no secret values in runner/history frames: pass. (g) Tree method column preserved; sidebar adds `R Run collection` hint: pass. (h) Borders on confirm modals only: pass.
- Tests: `go build ./... && go test -race ./... && golangci-lint run` green (0 issues). Commits `529a8bc` (runner), `e349d06` (history).
- Blockers / questions: none
- Next: none

### 2026-09-30 03:10 IST · WS6c · Composer 2.5 (fast) · PROGRESS
- Branch/worktree: ws/6c-runner-history
- Did: Fixed runner deviations: `RunInput.NoPersist` and `OnlyRequests` in the engine (with tests), wired setup “Persist variables” and “Re-run failed”, documented `OnlyRequests`/`setNextRequest` in `docs/engine.md`, and JSON export now uses a prefilled editable path plus a toast with the absolute path.
- Files: `internal/engine/{run.go,persist.go,session.go,run_options_test.go}`, `internal/tui/runner/runner.go`, `docs/engine.md`, `docs/ledger.md`
- Tests: `go build ./... && go test -race ./... && golangci-lint run` green (0 issues). Commit `bcaa029`.
- Blockers / questions: none
- Next: merge ws/6c-runner-history
