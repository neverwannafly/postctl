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
| WS3 script runtime | Opus 5 Thinking (high) | Running | ws/3-script | 2026-09-29 23:35 IST |
| WS4 engine/runner | — | Not started | — | — |
| WS5 UI components + TUI shell | Sonnet 5.5 (high) | Done, merged to main | ws/5-tui-shell | 2026-09-30 00:15 IST |
| WS6a request editor + send wiring | Sonnet 5.5 (high) | Done, ready to merge | ws/6a-request-editor | 2026-09-30 02:15 IST |
| WS6b response viewer | Composer 2.5 (fast) | Running | ws/6b-response-viewer | 2026-09-30 00:20 IST |
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

### 2026-09-30 00:20 IST · WS5 · Orchestrator · PROGRESS
- Did: Verified `ws/5-tui-shell` (in scope; golden renders reviewed by hand, e.g. the delete dialog at 200×60 defaults to Cancel and names the folder and request count). Merged into `main`, resolving `go.mod`/`go.sum`/ledger conflicts and a duplicate global `--workspace` flag between WS1 and WS5 in `cmd/postctl/main.go`. Build, race tests and lint are green.
- Decisions: D5 (send keys) and D6 (persist fold state) answer WS5's questions. WS5's `deepCopy`/`LoadLenient`-probe stand-ins still work (the probe now finds the real method); their cleanup is assigned to WS6a. Added C2: `internal/tui/send.go` send contract.
- Next: Launch WS6a (Sonnet 5.5) and WS6b (Composer 2.5) in parallel. WS4 (engine) starts when WS3 merges and will implement `tui.Sender`.

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
- Tests: `go build ./... && go test -race ./... && golangci-lint run` green (0 issues). New: request pane (about 90 tests incl. autosave, undo, OAuth, curl, `$EDITOR`), env editor (22), cookie manager (7), shell send/save queue/screen hosting/blur hook, and about 110 goldens.
- Blockers / questions: (1) `Ctrl-Enter` only works where the terminal reports enhanced keys; `Ctrl-S` always sends (D5), and Ctrl-S is also XOFF on terminals with software flow control. (2) `View` of the env editor, cookie manager and textarea adjust scroll offsets, which is idempotent but not strictly pure. (3) The response pane is the placeholder until WS6b merges.
- Next: merge `ws/6a-request-editor` (no overlap with `internal/tui/response`; `cmd/postctl/main.go` is unchanged where WS6b edits). WS4 implements `tui.Sender` and swaps `newSender()`.
