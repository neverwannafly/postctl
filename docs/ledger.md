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
| WS3 script runtime | Opus 5 Thinking (high) | Done, awaiting merge | ws/3-script | 2026-09-30 01:52 IST |
| WS4 engine/runner | — | Not started | — | — |
| WS5 UI components + TUI shell | Sonnet 5.5 (high) | Running | ws/5-tui-shell | 2026-09-30 00:10 IST |
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
