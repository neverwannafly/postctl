# AGENTS.md — working on postctl

This is the canonical, tool-agnostic guide for any coding agent (Cursor, Claude Code, Codex, ...). `CLAUDE.md` and `.cursor/rules/` only point here. Nested `AGENTS.md` files hold package-local rules; read the one for each directory you edit.

postctl (module `github.com/shubham/postctl`, Go 1.26, macOS only, pure Go, no cgo) is a keyboard-first terminal replacement for Postman: a Bubble Tea v2 TUI plus a linear CLI (`ls`, `send`, `run`, `env`, `curl`, `import`) over one shared engine. A workspace is a git-friendly tree of YAML files with sibling `.js` scripts; secret values live only in the macOS Keychain. Imported Postman scripts run in a goja sandbox with a Postman-compatible `pm.*` API. Several agents work in parallel, one workstream per worktree and branch.

Read first: [`CONTRIBUTING.md`](CONTRIBUTING.md), [`docs/onboarding.md`](docs/onboarding.md), [`docs/architecture.md`](docs/architecture.md) (layers, contracts, wiring, flows in `docs/flows/`), and **[`docs/plan.md`](docs/plan.md) §0, whose decisions are binding**. Also: your brief in `docs/briefs/`, [`docs/model.md`](docs/model.md), [`docs/engine.md`](docs/engine.md), [`docs/scripting.md`](docs/scripting.md), [`docs/tui-stack.md`](docs/tui-stack.md) and [`docs/design-language.md`](docs/design-language.md) (required for any visible change). If a request conflicts with a document, say so and update the document in the same change; never create a second convention. Changing a §0 decision needs the user.

## Repo map

| Path | What lives there |
| --- | --- |
| `cmd/postctl` | entry, global flags, `import` command (`import.go`), TUI wiring (`wire.go`: `lazyEngine`, pane factories) |
| `cmd/genkeys` | generates `docs/keys.md` from its hand-kept `KeyCatalog` (`go generate ./cmd/genkeys`) |
| `cmd/postman-extract` | dev tool: local Postman cache to Collection v2.1 JSON |
| `internal/model` | pure data; struct tags are the on-disk format; `Clone`/`CloneFresh`, `Validate` |
| `internal/store` | discovery, YAML/JS load and save, lock, atomic writes, migrations, `SecretStore`, Keychain, `.postctl/state.yaml` |
| `internal/vars` | scopes, `{{var}}` and `{{$dynamic}}` resolution, inherited-auth flattening (contract: `scope.go`) |
| `internal/httpx` | sender, bodies, auth, cookie jar, timings, curl (contract: `types.go`) |
| `internal/oauth2` | grants, loopback callback, token `Source` |
| `internal/script` | goja sandbox, `pm.*` (JS in `internal/script/assets/sandbox/`, Go bridge), limits |
| `internal/engine` | send pipeline, collection runner, persistence of variable writes |
| `internal/history` | bounded JSONL log and the `Redactor` |
| `internal/report` | JSON and JUnit run reporters (used by CLI and TUI runner) |
| `internal/wsx` | WebSocket client (handshake prep, connection events) |
| `internal/importer/postman`, `.../postmanlocal` | Postman v2.x JSON to model; local IndexedDB extractor to v2.1 JSON |
| `internal/cli` | linear commands, reporters, exit codes (plan §9: 0 ok, 1 tests failed, 2 request error, 3 usage) |
| `internal/ui` | Clear Signal theme tokens, glyph sets, shared components; no app logic |
| `internal/tui` | shell (layout, tree, palette, help, save queue, watcher); panes and screens in subpackages `request`, `response`, `envedit`, `cookies`, `runner`, `history`, `wspane`; `tuiengine` adapts the engine; `fakesend`, `fakerun`, `tuitest` are fakes |
| `internal/qa` | repo-level tests: secret-leak sweep, docs reference checks |
| `examples/httpbin`, `internal/tui/testdata/demo` | synthetic workspaces for hand runs and tests |

**Dependency direction.** `model` and `ui` import nothing internal. `store`, `vars`, `httpx`, `history` import only `model`. `script` uses `httpx`, `model`, `vars`; `oauth2` uses `httpx`, `model`, `store`; `wsx` uses `httpx`, `model`, `vars`. `engine` uses all of those and **must never import `internal/tui`, `internal/ui` or any Charm module**. `report` and `cli` build on `engine`. The TUI sends only through `tui.Sender` (implemented by `internal/tui/tuiengine`); the root `internal/tui` package imports `engine` only for runner event types. `cmd/postctl` is the only place that wires everything. `internal/importer/postman` imports `model` and `postmanlocal` only and never touches the Keychain; `cmd/postctl/import.go` saves through `store`. Check with `go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/...`.

## Setup and commands

Needs Go 1.26, `golangci-lint` 2.x and [Task](https://taskfile.dev/); node/npm only for `task assets`. Run everything from the repo root (or your worktree).

```sh
go build ./...                          # compile
go test -race ./...                     # full suite, ~30 s with a warm cache
golangci-lint run                       # must report 0 issues
task agents:check                       # the three gates above in one go
go test -race -count=1 ./internal/engine -run TestName   # focused run
task build                              # bin/postctl (version via -X main.version)
task keys                               # = go generate ./cmd/genkeys, rewrites docs/keys.md
task tui:smoke                          # build, then drive the TUI through a pty, offline
```

- If lint says `parallel golangci-lint is running`, another agent's run holds the cache lock: wait, or pass `--allow-parallel-runners` (`task agents:check` does).
- **Golden files.** Two mechanisms; `go test ./... -update` fails in packages without the flag. Run the package you changed, then read the diff:
  `go test ./internal/store -run TestGolden -update`; `go test ./internal/script -run TestCorpus -update`; `go test ./internal/ui ./internal/tui ./internal/tui/request ./internal/tui/response ./internal/tui/wspane -update`; `UPDATE_GOLDEN=1 go test ./internal/importer/postman ./internal/tui/runner`; `POSTCTL_GEN_DEMO=1 go test ./internal/tui -run TestGenerateDemoFixture` (rewrites `internal/tui/testdata/demo`).
- **Throwaway workspace.** `--workspace DIR` (or `-w`, or `POSTCTL_WORKSPACE`) skips discovery and the `~/.config/postctl` fallback. Never point experiments at a real workspace.
  ```sh
  tmp=$(mktemp -d); cp -R examples/httpbin/. "$tmp/"
  go run ./cmd/postctl ls --workspace "$tmp"                         # linear CLI, no TTY needed
  POSTCTL_FAKE_SENDER=1 go run ./cmd/postctl --workspace "$tmp"      # TUI (needs a TTY); sends answer from a canned fake, no network
  ```
  The CLI has no fake sender: `postctl send` goes to the network, so point `--var baseUrl=http://127.0.0.1:PORT` at a local `httptest`-style server. TUI checks without a human: golden tests, or `task tui:smoke` (`scripts/tui_smoke.py` copies a synthetic workspace, opens a request, presses Ctrl-S, prints PASS or FAIL only).
- **Private-data tests** read the gitignored `testdata/private/staging` at the repo root (and `internal/importer/postmanlocal/testdata/private`). They skip when it is absent (`TestPrivateCollections`, `TestPrivateStaging`, `TestPrivateIndexedDB`); `TestPrivateStagingLs` in `internal/cli` reads a hard-coded main-checkout path. Worktrees do not have the data. To run them there, `cp -R` the directory in; a symlink works too (both are ignored). Output must stay counts and names only.
- `POSTCTL_KEYCHAIN_TESTS=1` enables a test against the **real** Keychain (`internal/store`). Do not set it.
- `task assets` rebuilds embedded JS bundles and needs node 22: the default node on the dev machine is older, so run `nvm use 22` first. The output is reproducible (no diff against the committed bundles). Editing `internal/script/assets/sandbox/*.js` does not need it.

## Definition of done

1. `task agents:check` is green: `go build ./...`, `go test -race ./...`, `golangci-lint run`.
2. New behavior has tests (table-driven, synthetic data, fakes), including cancellation and secret masking where relevant. A bug fix starts with a failing test.
3. Goldens you changed are regenerated and the diff reviewed; `docs/keys.md` regenerated if any key changed.
4. Docs updated: the user-facing one (`docs/cli.md`, `docs/keys.md`, `docs/scripting.md`, `docs/model.md`, `docs/engine.md`) and `README.md` when behavior or flags change. Every exported identifier has a godoc comment starting with its name; every package has a package comment (revive enforces).
5. No secret or real data anywhere (code, fixtures, goldens, logs, ledger, commit messages). Any new output path is added to the `internal/qa` leak sweep.
6. Contracts changed additively only; no unrelated files touched; `go mod tidy` leaves no diff; a new dependency is pure Go and recorded in `docs/tui-stack.md`.
7. Ledger entry and status-board row written; committed on your branch; not pushed.

## Architecture invariants

- **Send pipeline order** (`docs/engine.md`, plan §4.1): build scope chain; pre-request scripts collection, folders (outermost first), request, one `script.Run` per level, scripts see the **unresolved** request; `vars.ResolveRequest` (flattens inherited auth; unresolved `{{x}}` become warnings); send through `httpx` with the cookie jar and the stored OAuth2 token (the engine never fetches a token); test scripts in the same order; persist changed variables; `history.Append` (redacted); `Outcome`. A skipped request or failed pre-request script writes no history.
- **Variable precedence**, lowest to highest: global < collection < folder < environment < data row < local. `--var` and `pm.variables.set` are the local layer. Folder variables are a read-only layer (`vars.FolderVariables`, `MemStore.SetFolderVariables`). The TUI preview, `postctl curl` and the engine must resolve identically (`TestScopeMatchesEngineFolderPrecedence`, `TestLocalVarsBeatFolderVariables`). `pm.environment`, `pm.collectionVariables` and `pm.globals` writes persist (plan R3); `pm.variables` does not.
- **The store is the only writer** of workspace files. Every writer holds one lock (per-directory mutex plus `flock` on `.postctl/store.lock`; not reentrant). Writes are atomic (temp file plus rename), unchanged files are not rewritten, output is deterministic. Code that changes some variables (engine, `env set`) uses `Update{Environment,Collection,Globals}Variables`, which re-read under the lock and apply only changed keys; it never writes back a stale snapshot. The TUI loads with `LoadLenient` and saves never delete files that failed to load. Changing a `model` struct tag is a schema change: migration in `internal/store` plus golden update.
- **Secrets**: values only in `store.SecretStore` (Keychain in production via `/usr/bin/security -i` on stdin, never argv; `store.MemorySecrets` in tests); YAML holds `secretRef` only; `Store.Load` never reads the Keychain, hydrate on clones; no plaintext fallback. Secrets never travel on argv in docs or examples: `env set <env> <key> --secret` prompts without echo (or reads one stdin line). The importer marks credential-looking variable names secret (plan D11). Every output path (history, reports, curl, pager, console, Timeline, tests, errors) masks through `history.Redactor` (`engine.Outcome.Redactor` carries it) unless the user explicitly reveals. The history redactor must know env, collection, folder and global secret values and OAuth token strings.
- **`View` is pure.** No I/O, no mutation (including scroll offsets); effects run as `tea.Cmd` returning typed messages; panes persist through `Backend` from commands only.
- **Contracts, additive changes only**: `internal/httpx/types.go` (`Sender`, `Response`, `Timings`, `SentRequest`), `internal/vars/scope.go` (`Scope`, `Store`, `Change`), `internal/tui/send.go` (`Sender`, `SendRequest`, `SendResult`). Renames or removals need the orchestrator (plan C1, C2).
- Scripts always have a timeout and a cancellable context; a runaway script must never freeze the UI. WebSocket requests (`kind: websocket`) are skipped by runs and rejected by `postctl send` (exit 3); anything that iterates requests must handle `model.KindWebSocket`.

## Security and privacy (non-negotiable)

- **Never** print, log, commit, or put in the ledger, chat, tests, fixtures or goldens: variable values, tokens, header values, request or response bodies, cookies, or URLs with query strings. Log counts, key names and structure only. Failure messages in tests must not echo data either.
- Committed fixtures are **synthetic** (`example.test` hosts, invented tokens, generated IDs). `testdata/private/` is gitignored and holds the only real data; tests using it skip when absent.
- **Never touch the real Keychain in tests.** Inject `store.MemorySecrets`.
- Real Postman data is company data: read only locally, read-only, from a temp copy (plan R4); report counts and structure.
- Redact before display or persistence, not after. `Outcome.Resolved` can hold secrets: never log it.
- Only commit files you created or intended to change; check `git status` for strays (private data, `bin/`, `.postctl/`).

## Testing patterns

- Table-driven tests with fakes: `store.NewMemorySecrets()`, `httptest` servers (never the public network), `t.TempDir()` workspaces, `tuitest.Demo()` and `tuitest.Write` for TUI workspaces, `fakesend`/`fakerun`, engine helpers `newFixture`/`newServer`, CLI helper `demoWorkspace`.
- **Leak sweep** `internal/qa/secret_leak_test.go` (`TestSecretDoesNotLeakAcrossPersistenceReportsCurlOrTUI`): unique sentinel secrets go through send, run, history, JSON/JUnit reports, masked curl, pager and 140x40 frames, then are searched for in every artifact. When you add an output path (a new screen, reporter, export, log, error text), extend it, and prove it by temporarily removing your masking.
- Purity: each pane has a `TestViewIsPure` style test (call `View` twice, compare the model). Key reference: `cmd/genkeys` tests and `internal/tui/keys_test.go` compare `docs/keys.md` with code.
- Golden renders at 80x24, 120x40 and 200x60 in dark, `NO_COLOR` and ASCII looks.
- Real-data tests must run **the code path the user runs** (for example `import postman-local` end to end), not a staged copy, and assert non-zero counts. See the importer lesson below.
- `internal/qa/docs_paths_test.go` checks that paths, test names, env vars and task names mentioned in agent docs exist; it runs with `go test ./internal/qa`.

## Design-language rules for TUI work

Full spec: `docs/design-language.md`. Musts: semantic tokens from `internal/ui` only (no one-off colors); **never color alone** (method text, status code and reason, glyph or prefix); `NO_COLOR`, `--ascii`, `--accessible` work; usable at 80x24, reflow at the 100 and 140 column breakpoints; keyboard-only; active environment always visible, `! prod` badge for production, sending never asks for confirmation; every empty, error and waiting state says what happened and the next action; vim keys per plan U1 and the design-language key map (modal text fields, vim motions in lists, `Space` leader with which-key; `Ctrl-S`/`Ctrl-Enter` send in every mode, `Ctrl-P` palette, `Esc` ladder back); every key that needs enhanced keyboard reporting has a legacy fallback (U4); footer shows only valid hints; secrets render `••••` (ASCII `****`); destructive dialogs default to Cancel. Regenerate `docs/keys.md` after any key change. Do not advertise a key or action that is not wired (hide it, as nil `Options` factories do).

## Multi-agent workflow

- One workstream, one worktree, one branch: `git worktree add ~/work/postctl-wt/<name> -b <type>/<name> main` (prefixes seen: ws, fix, qa, docs). Stay inside your brief's file ownership; ask for cross-workstream changes through the ledger. Do not push. The orchestrator merges unless told otherwise.
- **Ledger** (`docs/ledger.md`) is append-only: add `### <YYYY-MM-DD HH:MM IST> · <WS id> · <agent/model> · <STARTED|PROGRESS|BLOCKED|DONE|HANDOFF>` with Branch/worktree, Did, Files, Decisions, Tests, Blockers / questions, Next; at start, milestones, blockers and end. Never edit someone else's entry (add a correction entry). Keep your status-board row current. Counts and names only.
- Merge with `git merge --no-ff <branch>` into `main` (from `/Users/shubham/work/postctl`), then gate `main`, then `git worktree remove <path>`. `docs/ledger.md` conflicts on every merge; resolve with `python3 /Users/shubham/work/postctl-wt/.orchestrator/resolve_ledger.py docs/ledger.md "<status row prefix>" "<full new row>"` (keeps both sides' entries and main's rows). For `go.mod`/`go.sum` conflicts keep both sides' requirements, then run `go mod tidy`. After a merge re-run the gate and `go run ./cmd/postctl --help` (duplicate flag definitions panic at start-up).
- Decisions live in `docs/plan.md` §0. If one seems wrong, write it under "Blockers / questions" and take the least surprising option.

## Known pitfalls (each one happened)

1. **Importer type mismatch lost all scripts.** `import postman-local` dropped every script: the extractor (`internal/importer/postmanlocal/extract.go`) emitted `exec` as Go `[]string`, while `internal/importer/postman/converter.go` only accepted JSON-shaped `[]any` and treated anything else as empty (fixed in both places: `shapeDocument` on the producer side, normalization in `ConvertObject`). Tests fed the converter JSON files (which decode to `[]any`), never the extractor's output. **Rules:** values crossing importer or decoder boundaries are JSON-shaped (`map[string]any`, `[]any`, `string`, `float64`, `bool`, `nil`) or normalized at the boundary; never treat an unknown dynamic type as "empty" silently (report it); real-data tests exercise the same path as the command and assert non-zero counts.
2. **Secrets leaked through secondary views**: Timeline URL and response headers, the pager temp file, the TUI Console and Tests tabs, OAuth tokens missing from the redactor's value list, and a folder secret. Route every new output through `history.Redactor`; extend the leak sweep.
3. **Env/globals persistence races**: the engine wrote its start-of-send snapshot over concurrent TUI and CLI edits. Use the store lock and `Update*Variables`; never `Save*` a stale copy.
4. **Folder-variable precedence split**: preview resolved folder variables but send did not. One resolution path (`vars.FolderVariables`); parity test.
5. **State mutated inside `View`** (scroll offsets in `envedit`, `cookies`, textareas). Adjust in `Update`/`SetSize`; add a purity test.
6. **Key reference drift**: a documented `e` key did nothing; the catalog in `cmd/genkeys/main.go` is hand-maintained, so a new key needs an entry there and `task keys`. Both test suites fail on drift.
7. **Misleading no-op UI** that reviewers rejected: dead keys, a runner "Pause" with no engine support, a "Persist variables" toggle that was ignored, empty states with no next action. Wire it, or hide it and say so in the ledger.
8. **Goja sandbox**: a timeout is not enough (native calls, regexp backtracking, recursion and memory needed their own limits); bootstrap functions were reachable as globals. Do not add globals or bridge functions without the guard tests (`internal/script/AGENTS.md`).
9. **WebSocket requests in runs** produced transport errors until runs skipped them; new request-iterating code must handle `KindWebSocket`.
10. **Duplicate `--workspace` flag** defined on both sides of a merge; `go mod tidy`, ledger and `go.sum` conflicts on every merge (see workflow).
11. **Revive lint**: exported identifiers and packages without doc comments failed after a rule was enabled. Comments start with the identifier name. Tests are exempt from `errcheck`, `gocritic` and `revive` only.
12. **Private data in worktrees**: the old `.gitignore` pattern `testdata/private/` was anchored to the repo root and missed `internal/importer/postmanlocal/testdata/private`; it is now `**/testdata/private`. Keep it that way.
13. `Ctrl-S` is XOFF on terminals with flow control and `Ctrl-Enter` needs enhanced keyboard reporting; `Ctrl-S` (and Enter in the URL bar) must always send.

## Where to add things

- **CLI command**: `internal/cli/` a new file (`runX(ctx, a *app, args) int`; use `a.flagSet`, `a.parse`, `a.open`, `a.mask`), add to the `commands` table in `internal/cli/cli.go` **and** the `commands` table plus the `case "ls", "send", ...` list in `cmd/postctl/main.go` (keep usage strings identical), `--json` output, exit codes 0/1/2/3. Tests: `internal/cli/cli_test.go` (httptest plus temp workspace, mask and exit-code cases), `cmd/postctl/main_test.go`. Docs: `docs/cli.md`, `README.md`.
- **TUI pane or screen**: package under `internal/tui/<name>/` implementing `tui.Pane` or `tui.Screen` (`internal/tui/pane.go`); add an `Options` factory in `internal/tui/app.go` (nil hides it), set it in `cmd/postctl/wire.go`; open via palette `Registry` action, `tui.OpenScreenMsg` or a key in `internal/tui/keys.go`; help text in `internal/tui/help.go`; entry in `KeyCatalog` (`cmd/genkeys/main.go`) then `task keys`. Tests: goldens (3 sizes, 3 looks), `TestViewIsPure`, fits-size, no-secret frame; extend `internal/qa`. Read `internal/tui/AGENTS.md`.
- **`pm.*` API**: JS in `internal/script/assets/sandbox/NN-*.js` (filename order is load order); Go bridge function only if needed (`internal/script/bridge.go`, and add its name to `bridgeFunctionNames` in `internal/script/sandbox_globals_test.go`); a case in `internal/script/testdata/corpus/` plus `TestCorpus -update`; a unit test; compatibility table in `docs/scripting.md`. No new globals.
- **Importer field**: field in `internal/model` (with `Clone`, `Validate`, YAML tag, store golden), mapping in `internal/importer/postman/converter.go` (unknown data into `Extra`), extractor output in `internal/importer/postmanlocal/extract.go` only as JSON-shaped values, tests for **both** the JSON-file path and the `postman-local` path with non-zero counts (`UPDATE_GOLDEN=1` for the golden workspace), table in `docs/model.md`. Read `internal/importer/AGENTS.md`.
- **Auth type**: `internal/model/auth.go` (type, struct, `Clone`, `Validate`), `internal/vars/resolve.go` (inheritance), `internal/httpx/auth.go` and `internal/httpx/curl.go` (apply, curl), `internal/tui/request/auth.go`, `internal/script/convert.go` (`pm.request.auth`), importer mapping and its secret move (`moveAuthSecrets` in `internal/importer/postman/secrets.go`), history redaction names, store round-trip golden, leak sweep, `docs/model.md`.
- **Model or on-disk field**: tag change is a schema change; `internal/store` golden (`TestGolden -update`), migration hook if it is not purely additive, Clone test passes (reflection-driven), `docs/model.md`.
