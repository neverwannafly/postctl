# postctl — plan (v1, answers recorded 2026-09-29)

> Status: **approved answers in [§0](#0-decisions-locked-2026-09-29) override anything below marked `❓`.** Every agent must read this file, [`design-language.md`](design-language.md) (to be ported by WS0), its own brief in `docs/briefs/`, and append to [`ledger.md`](ledger.md).

## 0. Decisions locked (2026-09-29)

| # | Topic | Decision |
| --- | --- | --- |
| Q1 | Storage | Directory tree, one YAML file per request, scripts as real `.js` files (§5). |
| Q2 | Location | Both: global default workspace `~/.config/postctl/`, plus project-local workspaces discovered by walking up from cwd to a `postctl.yaml`. |
| Q3 | Import sources | Exported Collection v2.1/v2.0 JSON files **and** (per Q15) extraction from the local Postman desktop install. Environments/globals come along from the local install. No Postman API, no curl/OpenAPI import in v1. Re-import: overwrite by stable ID with `--dry-run` diff (merge not required). |
| Q4 | Scripts | Tier T1 + T2 (§7). T3 legacy globals: best-effort only if cheap; `pm.visualizer`/`pm.vault` no-op with warning. |
| Q5 | Editing | Inline editor **and** `e` → `$EDITOR` round-trip. |
| Q6 | Platform | macOS only (arm64 + amd64). Keep OS-specific code behind interfaces anyway. |
| Q7 | Secrets | Variables can be marked secret; values stored in macOS Keychain, files hold only a reference. |
| Q8 | v1 scope | CLI runner (json/junit), TUI collection runner, history, cookie jar, OAuth2, **WebSocket + gRPC**, copy-as-curl/codegen, command palette. **Out:** export to Postman, GraphQL-specific UI (GraphQL bodies imported/sent as JSON). |
| Q9 | Layout | Responsive: 3 panes wide, stacked/tabbed medium, single pane + overlays at 80 cols. |
| Q10 | Prod guard | Environments flagged `production` show a warning badge only; no send confirmation. |
| Q11 | Keys | Standard/arrow keys like prod-console-tui (no vim bindings). `Ctrl-Enter`/`Ctrl-S` send, `Ctrl-P` palette. |
| Q12 | Distribution | Local builds only (`go install ./cmd/postctl`); module path `github.com/shubham/postctl` (placeholder, cheap to change). No GoReleaser yet. |
| Q13 | Git | `git init`; one branch + worktree per workstream; orchestrator merges. |
| Q14 | Models | §10 mapping accepted ("Luna 6" unavailable). |
| Q15 | Fixtures | Pull from the local Postman install. Findings below. |
| R1 | gRPC | **Deferred to v1.1.** WS9 is WebSocket only in v1. Keep `model.Request` kind-extensible so gRPC slots in later. |
| R2 | OAuth2 | Client credentials, authorization code + PKCE (browser + `127.0.0.1` loopback callback), password, implicit. No automatic refresh in v1; user triggers "Get new token". Tokens are secrets (Keychain). |
| R3 | Script var writes | `pm.environment/collectionVariables/globals.set` persist to disk (Postman behavior); secret vars persist to Keychain. `pm.variables.set` stays local to the run. |
| R4 | Local Postman data | WS1b may read the IndexedDB locally, read-only, from a temp copy. Never print variable values, tokens, headers values, or bodies in logs, ledger, or chat; only counts, key names, and structure. |

### Q15 findings — local Postman install

- `~/Library/Application Support/Postman/backup-*.json` exist but are empty (169 bytes: 0 collections, 1 environment).
- Real data is in Chromium IndexedDB under `Partitions/<uuid>/IndexedDB/https_desktop.postman.com_0.indexeddb.leveldb` (~3 MB, ~636 `collectionId` references) and `Partitions/<uuid>/IndexedDB/file__0.indexeddb.{leveldb,blob}` (~3.7 MB).
- Format: LevelDB with Chromium's `idb_cmp1` comparator; values are V8 structured-clone serialized (possibly Snappy-compressed, large values spilled to `.blob`). Postman may keep the DB locked while running → always copy the directory to a temp dir first and open read-only.
- It is a *cache* of cloud-synced workspaces: may be partial. The extractor must report coverage (collections found, requests per collection, missing bodies) so gaps are visible, and the fallback is "File → Export" in Postman.
- Design: new workstream **WS1b** builds `postctl import postman-local` that emits standard Collection v2.1 / environment JSON into a staging dir, which WS1's importer then consumes. This keeps the fragile, reverse-engineered part isolated from the stable importer.
- Real data is company data: extracted fixtures live in `testdata/private/` (gitignored), never committed; tests using them are skipped when absent.

## 1. Goal

`postctl` is a keyboard-first terminal replacement for Postman, written in Go. It should feel as calm and exact as `~/work/prod-console-tui` (Clear Signal design language), but for API development:

- Browse collections/folders/requests in a tree, edit a request, send it, inspect the response — without leaving the terminal.
- Import existing Postman collections, environments, and globals **including pre-request and test (post-response) scripts**, and run those scripts with a Postman-compatible `pm.*` API.
- Store everything as plain, diffable files so collections can live in git next to the code they exercise.
- Offer a linear/scriptable CLI (a `newman`-style runner) for CI and accessible use, sharing the same engine as the TUI.

"Surreal dev experience" translated into concrete requirements:

| Requirement | Concrete meaning |
| --- | --- |
| Instant | Cold start to usable tree < 150 ms for 1,000 requests; send is one key (`Ctrl-Enter` / `Enter` on request). |
| Zero-friction onboarding | `postctl import postman ~/Downloads/*.json` and you're working. First run with no data shows a one-screen guide, not an empty box. |
| Never lose work | Edits autosave to disk (atomic write); undo within session; history of every sent request. |
| Discoverable | `?` help per screen, command palette (`Ctrl-P` / `:`) with fuzzy search over requests and actions. |
| Honest | Every running state is explained (`… Running pre-request script`, `… Waiting for response 1.2s`). Errors say what happened and what to do. |
| Scriptable | Everything in the TUI has a CLI equivalent with `--json` output. |
| Safe | Secrets never printed, logged, or written in plaintext unless the user opts in. |

## 2. Non-goals (v1)

- Postman cloud sync, team workspaces, mock servers, monitors, API documentation publishing.
- A browser-like HTML renderer for responses.
- Full Node.js compatibility in scripts (no `fs`, no arbitrary npm modules).
- Okta/SSO integration (explicitly excluded — the reference repo's `okta.md` does not apply).
- ❓ gRPC, WebSocket, Socket.IO, MQTT requests (see Q8).

## 3. Reference and design language

Source: `~/work/prod-console-tui/docs/{design-language.md,tui-stack.md}`. We adopt, with these adaptations:

- **Theme:** Clear Signal tokens verbatim (`text.*`, `accent.primary`, `status.*`, `border.subtle`, `surface.selected`), light/dark detection via Bubble Tea, `NO_COLOR`, `--ascii`, `--accessible`.
- **Added tokens** (to be ratified in our design doc): HTTP method colors (`method.get` = info, `method.post` = success, `method.put/patch` = warning, `method.delete` = danger, others = secondary) — always shown with the method *text*, never color alone; syntax tokens for JSON (`syntax.key`, `syntax.string`, `syntax.number`, `syntax.bool`, `syntax.null`) derived from the existing palette; response status classes 2xx/3xx/4xx/5xx mapped to success/info/warning/danger.
- **Layout deviation (❓ Q9):** the reference says "one column at 80 cols". Postman is inherently multi-pane. Proposal: responsive layout —
  - ≥ 140 cols: three panes `Sidebar | Request | Response`.
  - 100–139 cols: `Sidebar | Request/Response stacked or tabbed`.
  - < 100 cols: single pane with focus switching (`Tab` / `1 2 3`), sidebar as an overlay.
- Header always shows: product mark, **active environment** badge (this is our equivalent of "keep production context visible" — e.g. `● prod` in warning color when an environment is flagged as production), and workspace name.

Sketch (wide):

```text
postctl  payments-api                                   env  ● staging
──────────────────────────────────────────────────────────────────────────────────────────
Collections          │ POST  {{baseUrl}}/v1/charges                    │ 201 Created  184 ms  1.2 KB
› ▾ Payments         │ Params  Auth  Headers(3)  Body  Pre  Tests      │ Body  Headers(9)  Tests 3/3
    GET  List charges│ ─────────────────────────────────────────────── │ ───────────────────────────
  › POST Create      │ {                                               │ {
    DEL  Refund      │   "amount": {{amount}},                         │   "id": "ch_8f2c",
  ▸ Customers        │   "currency": "usd"                             │   "status": "succeeded"
                     │ }                                               │ }
──────────────────────────────────────────────────────────────────────────────────────────
Ctrl-Enter Send  e Edit in $EDITOR  E Environments  / Filter  Ctrl-P Palette  ? Help
```

## 4. Architecture

Stack (inherits reference `tui-stack.md` policy: stable v2 Charm modules pinned together, stdlib-first):

| Area | Choice | Notes |
| --- | --- | --- |
| TUI | `charm.land/bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`, `huh/v2` | Same versions as prod-console-tui. |
| JS runtime | [`github.com/dop251/goja`](https://github.com/dop251/goja) (pure Go, ES5.1+ most ES2015+) + `goja_nodejs` for `console`, `require` registry | No cgo, single static binary. Alternative: embed QuickJS via wasm (heavier). |
| Assertions | Chai-compatible `expect` subset implemented in JS/Go shim, or bundle `chai` UMD into goja | ❓ Q4 decides depth. |
| HTTP | `net/http` with custom transport for timing (DNS/connect/TLS/TTFB via `httptrace`), cookie jar, proxy, client certs | |
| Storage | ❓ Q1/Q2 — proposed: YAML-per-request directory tree | |
| Secrets | macOS Keychain via small interface (same approach as reference `internal/credentials`) | ❓ Q7 |
| Syntax highlight | [`github.com/alecthomas/chroma/v2`](https://github.com/alecthomas/chroma) mapped onto Clear Signal syntax tokens | |
| JSON query | `gjson` / built-in path filter in response viewer | |
| Release | GoReleaser, `CGO_ENABLED=0` | ❓ Q12 |

Package layout (proposed):

```text
cmd/postctl                 entry, flags, wiring, version
internal/model              Collection, Folder, Request, Body, Auth, Variable, Environment, Script (pure data, no I/O)
internal/store              load/save workspace on disk, atomic writes, file watching, migrations
internal/importer/postman   Postman Collection v2.0/v2.1, environment, globals → internal/model
internal/exporter           (later) model → Postman v2.1, curl
internal/vars               variable scopes + {{var}} / {{$dynamic}} resolution
internal/httpx              request builder, auth application, transport with timings, cookie jar, response model
internal/script             goja sandbox, pm.* API, console capture, test results, timeouts
internal/engine             orchestrates one "send": resolve → pre-scripts (collection→folder→request) → send → test scripts → results; also collection runner
internal/history            sent-request log (bounded, on disk)
internal/secrets            Keychain-backed secret variable storage
internal/ui                 theme tokens, shared components (status, empty, error, key help, method badge, JSON view)
internal/tui                root model, panes, screens, modals, palette, editors
internal/cli                linear commands: import, run, send, env, ls, export
```

Rules (carried from reference AGENTS.md):

- Views render state only; HTTP, scripts, disk, Keychain run as `tea.Cmd`s returning typed messages.
- `internal/engine` has **zero** TUI imports; TUI and CLI are both thin clients over it.
- Scripts run with a hard timeout and cancellable context; a runaway script never freezes the UI.
- Semantic theme tokens only; no one-off colors.

### 4.1 Send pipeline

```text
select request
  → build scope chain (globals < collection < environment < data row < local)
  → run pre-request scripts: collection → folder(s) → request   (can mutate request + vars)
  → resolve {{vars}} and {{$dynamic}} in URL, headers, body, auth
  → apply inherited auth
  → send (httptrace timings, redirects per setting, cookie jar)
  → run test scripts: collection → folder(s) → request            (pm.response, pm.test)
  → persist variable mutations (env/collection/globals) per settings
  → record history entry
  → emit Result{response, tests, console logs, timings, errors}
```

## 5. Data model and storage (❓ Q1, Q2)

Proposed on-disk layout (Bruno-like, git-friendly):

```text
<workspace>/
  postctl.yaml                   workspace metadata, settings
  collections/
    payments/
      collection.yaml            name, description, auth, variables, pre/test script refs
      scripts/pre.js  tests.js   collection-level scripts
      charges/
        folder.yaml              folder auth, vars, scripts
        001-list-charges.yaml    one request per file (method, url, headers, body, auth, scripts inline or referenced)
        002-create-charge.yaml
  environments/
    staging.yaml                 non-secret values; secret keys stored in Keychain by reference
  globals.yaml
  .postctl/                      local state: history, cookies, UI state (gitignored)
```

Scripts live as real `.js` files (editable with any editor, get LSP/syntax highlighting) referenced from YAML, or inline for short ones. Order is preserved via numeric prefixes or an explicit `order:` list.

## 6. Postman import

`postctl import postman <paths...> [--workspace DIR] [--env FILE...] [--globals FILE] [--dry-run]`

- Inputs: Collection v2.1 (primary) and v2.0 JSON exports; environment and globals exports; ❓ Postman API pull via API key (Q3); ❓ full "Export data" dump zip (Q3).
- Maps: folders (nested), requests, URL (raw + structured), query/path variables, headers (incl. disabled), bodies (raw w/ language, urlencoded, form-data incl. files, binary, GraphQL), auth (noauth, inherit, bearer, basic, apikey, digest, oauth2 config, awsv4, hawk → unsupported ones preserved + flagged), collection/folder/request variables, events (`prerequest`, `test`) with `script.exec` lines → `.js` files, saved example responses, descriptions.
- Report: summary table of imported / converted-with-warnings / unsupported items (e.g. "3 scripts use `pm.visualizer` — not supported, will no-op").
- Idempotent re-import: stable IDs from Postman `_postman_id`/`id`; ❓ merge vs overwrite (Q3).
- Static script lint on import: flag usage of APIs outside our supported `pm` surface.

## 7. Scripts (pre-request and post-response)

Postman-compatible API surface, tiered (❓ Q4 picks the target tier for v1):

| Tier | Surface |
| --- | --- |
| **T1 core** | `pm.environment/collectionVariables/globals/variables` `.get/.set/.unset/.has/.toObject/.replaceIn`, `pm.request` (url, headers, body, method; mutable in pre-request), `pm.response` (`code`, `status`, `headers`, `responseTime`, `responseSize`, `json()`, `text()`), `pm.test`, `pm.expect` (common chai chains), `pm.response.to.have.status/header/jsonBody`, `console.log/info/warn/error`, `pm.info` (requestName, iteration, eventName). |
| **T2 common** | `pm.sendRequest` (sync-looking callback + promise), `pm.cookies`, `pm.iterationData`, `pm.execution.skipRequest()`, `pm.execution.setNextRequest()` / legacy `postman.setNextRequest`, `require('lodash' | 'moment' | 'crypto-js' | 'uuid' | 'chai' | 'ajv' | 'xml2js')`, `btoa/atob`, `setTimeout`. |
| **T3 legacy/rare** | Legacy globals (`tests[]`, `responseBody`, `responseCode`, `postman.getEnvironmentVariable`…), `pm.vault`, `pm.visualizer` (no-op + warning), `pm.require` packages. |

Sandbox rules: no filesystem/network except `pm.sendRequest`; per-script timeout (default 5 s, configurable); memory guard via goja interrupt; console output captured into a "Console" pane and CLI output; script errors shown with file:line mapped to the `.js` file.

Compatibility test corpus: a fixture folder of real-world Postman scripts (and ❓ the user's own collections, Q15) with expected outcomes, run in CI.

## 8. TUI feature set (v1 target — trimmed by Q8)

- Sidebar: collection tree with fold/unfold, method badges, fuzzy filter, create/rename/duplicate/move/delete (delete confirms, defaults to Cancel).
- Request editor tabs: Params, Auth, Headers, Body, Pre-request, Tests, Settings. Inline key-value tables with enable/disable toggle. Body/script edit: inline textarea **and** `e` → `$EDITOR` round-trip.
- Response viewer tabs: Body (pretty/raw/preview-as-text, syntax-highlighted, collapsible JSON, `/` search, path filter), Headers, Cookies, Tests (pass/fail with messages), Console, Timeline (DNS/connect/TLS/TTFB/download).
- Environments: switcher (`E`), editor, secret values masked (`••••`) with reveal-on-hold, "production" flag that turns header badge warning-colored and asks confirmation before sending non-GET requests (❓ Q10).
- Multiple open request tabs; unsaved marker; autosave.
- Command palette (`Ctrl-P`): jump to request, run action, switch env.
- History pane: re-open/re-send past requests with their exact resolved values.
- Collection runner screen: select folder/collection, iterations, data file (CSV/JSON), delay; live progress with real per-request steps.
- Copy as curl; import from curl paste (`Ctrl-V` in URL bar detects curl).

## 9. CLI (linear, scriptable, accessible)

```sh
postctl                               # TUI
postctl import postman <files…>       # import
postctl ls [collection[/folder]]      # tree listing, --json
postctl send <collection/path/request> [-e env] [--var k=v] [--json]
postctl run <collection[/folder]> [-e env] [-d data.csv] [-n iterations] [--reporter cli|json|junit] [--bail]
postctl env ls|use|set|get
postctl export postman|curl <path>    # (later)
```

Exit codes: 0 all tests passed; 1 test failures; 2 request/transport errors; 3 usage/config errors.

## 10. Workstreams and agent assignment (proposed)

Each workstream gets a **brief** file `docs/briefs/WSn-<name>.md` (scope, interfaces it owns/consumes, acceptance criteria, test fixtures, files it may touch) so one agent can own it end to end. Agents work in isolated git worktrees/branches (`ws/<n>-<name>`) and log to `docs/ledger.md`.

| WS | Scope | Depends on | Proposed model | Why this model |
| --- | --- | --- | --- | --- |
| WS0 | Repo scaffold, `go.mod`, `internal/model`, `internal/store` (YAML load/save/atomic write), design doc port, CI (`go test`, `golangci-lint`), Taskfile | — | Sonnet 5.5 (high) | Foundational contracts; needs careful, readable types. |
| WS1 | Postman importer + fixtures + import report | WS0 model | GPT-5.6 Sol (medium) | Large, well-specified schema mapping; exhaustive edge cases. |
| WS1b | Local Postman extractor: Chromium IndexedDB (LevelDB `idb_cmp1` + V8 deserialization + blobs) → v2.1 JSON staging dir; coverage report | none (outputs standard v2.1 JSON) | GPT-5.6 Sol (medium) | Reverse-engineering binary formats; needs rigor and a spike first. |
| WS9 | WebSocket (connect, send/receive log, saved messages) and gRPC (unary + streaming, server reflection and `.proto` import) request types in engine + TUI | WS4, WS6 | Sonnet 5.5 (high) | Two protocols with distinct UX; integration-heavy. |
| WS2 | `internal/vars` + `internal/httpx` (auth, bodies, timings, cookies, redirects, proxy, TLS) | WS0 model | GPT-5.6 Sol (medium) | Protocol correctness, many small rules. |
| WS3 | `internal/script` goja sandbox + `pm.*` API + chai/lodash bundles + compat corpus | WS0, WS2 interfaces | Opus 5 Thinking (high) | Hardest piece: JS semantics, sandboxing, async `sendRequest`, compatibility. |
| WS4 | `internal/engine` send pipeline + collection runner + history | WS2, WS3 | Sonnet 5.5 (high) | Integration glue with careful ordering semantics. |
| WS5 | `internal/ui` theme + components + TUI shell (layout, panes, focus, keys, help, palette) | WS0 | Sonnet 5.5 (high) | Design-sensitive; must follow Clear Signal precisely. |
| WS6 | TUI request editor + response viewer (JSON highlight/fold/search) + env editor | WS4, WS5 | Composer 2.5 (fast) for components, reviewed | Well-bounded UI components once shell exists. |
| WS7 | `internal/cli` (`send`, `run`, reporters json/junit, `ls`, `env`) | WS4 | Grok 4.7 (high, fast) | Straightforward, fast iteration. |
| WS8 | README, docs, GoReleaser, Homebrew tap, sample workspace | all | Gemini 3.8 Flash (high) | Docs/config, low risk. |
| QA | Per-merge review: Bugbot-style review + integration smoke on the user's collections | each WS | Cursor Grok 4.6 (high, fast) / Bugbot | Independent second opinion. |

Waves: **W1** WS0 + WS1b spike (independent) → **W2** WS1, WS2, WS5 in parallel → **W3** WS3, WS4 → **W4** WS6, WS7 → **W5** WS9 → **W6** WS8 + QA hardening. WS8 drops GoReleaser/Homebrew (Q12: local builds only). The orchestrator (me) only writes plans/briefs, merges, resolves cross-workstream interface conflicts, and reports progress; it does not implement features.

## 11. Definition of done (v1)

- Import of the user's real Postman exports succeeds with a report; ≥ 95 % of scripts run unmodified (remaining ones flagged with reason).
- Send → response → tests loop works in the TUI at 80×24 and 200×60, dark/light, `NO_COLOR`, `--ascii`, `--accessible`.
- `postctl run` output matches newman pass/fail on the same collection for the compat corpus.
- No secret value appears in logs, history files, or screenshots of the UI by default.
- `go test ./...` green, race detector clean on engine/script packages.

## 12. Open questions

Tracked in chat; answers will be recorded here and this doc bumped to v1.

1. Storage format.
2. Storage location (global vs per-project workspace).
3. Import sources and re-import semantics.
4. Script compatibility tier for v1.
5. Script/body editing (inline vs `$EDITOR`).
6. Target platforms.
7. Secret variable storage.
8. v1 feature scope.
9. Layout at wide widths.
10. Production-environment safety guard.
11. Keybinding style.
12. Distribution / module path.
13. Git + parallel worktrees.
14. Model substitutions ("Luna 6" is not an available model).
15. Real Postman exports for testing.
