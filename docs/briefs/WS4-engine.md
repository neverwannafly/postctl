# WS4 — Engine: send pipeline, collection runner, history

Owner model: Sonnet 5.5 (high) · Branch: `ws/4-engine` · Worktree: `~/work/postctl-wt/ws4-engine`

## Goal

`internal/engine` is the one place that knows the order of operations. The TUI and CLI are thin clients over it. It glues together the pieces already on `main`: `vars`, `httpx`, `oauth2`, `script`, `store`.

## Read first

- `docs/plan.md` §0 (binding: R3 persistence of script var writes, D2 secrets, C1/C2 contracts), §4 rules (engine has **zero** TUI imports), §4.1 send pipeline, §9 exit codes.
- `docs/model.md` rules; `docs/scripting.md` (what scripts can do and how `Run` behaves); the DONE ledger entries of WS2, WS2b and WS3 (API and deviations).
- APIs on main:
  - `vars.New(globals, collection, environment []model.Variable, data map[string]any) *MemStore`, `vars.ResolveRequest(req, chain, store)`, `vars.Apply(ws, env, col, changes)`.
  - `httpx.NewClient(version)`, `(*Client).Send(ctx, req, httpx.Options)`, `httpx.NewCookieJar(workspaceDir)`, `httpx.Curl`, `Options{WorkspaceDir, MaxBodyBytes, Defaults, OAuth2 TokenSource, CookieJar}`.
  - `oauth2.NewSource(secrets, fetch)`, `(*Source).Token` (returns `oauth2.ErrNoToken` when missing or expired; never auto-fetches).
  - `script.Run(ctx, scripts []*model.Script, script.Input) (*script.Result, error)`; `Input{Event, Request, Response, Vars, Info, Sender, SendOptions, Cookies, Timeout}`; `Result{Request, Tests, Console, Err, NextRequest, SkipRequest, TimedOut}`.
  - `store.Store` (`Load`, `Hydrate*`, `SaveEnvironment`, `SaveCollection`, `SaveGlobals`, `SecretStore`).
  - `internal/tui/send.go`: the `tui.Sender` contract your adapter implements.
- Postman semantics to match: in pre-request scripts `pm.request` shows **unresolved** `{{vars}}`, and the request is resolved after pre-request scripts run. Script chain order is collection → folder(s) → request for both events. `pm.variables` (Local) is fresh per send, or per iteration in a run.

## Scope (files you own)

- `internal/engine/`: `Engine`, the send pipeline, the runner, and adapters (cookie adapter; see below).
- `internal/history/`: request history.
- `internal/tui/tuiengine/`: a tiny adapter package implementing `tui.Sender` on top of `*engine.Engine` (tui may import engine; engine must not import tui). **Don't** edit `cmd/postctl` or other `internal/tui` files: WS6a is editing them in parallel. The orchestrator wires your adapter in at merge time.

## Requirements

1. **Engine construction:** `engine.New(Config{Store, Secrets, HTTP httpx.Sender, Cookies *httpx.CookieJar, OAuth2 httpx.TokenSource, Version, Clock, ScriptTimeout})`, with defaults for everything except `Store`.
2. **Send** `(e *Engine) Send(ctx, SendInput{Collection, Request (possibly unsaved copy), Environment, Data map[string]any, Iteration}, progress func(Step)) (*Outcome, error)`. It implements plan §4.1 exactly:
   - Hydrate secrets for variables and auth on **copies** (`Clone`); never mutate caller models.
   - Build the vars store; find the folder chain for the request (`Collection.Find`/`Container`).
   - Run pre-request scripts with an unresolved clone. Honor `SkipRequest`. Take `Result.Request` as the request to send.
   - `vars.ResolveRequest` with the chain; unresolved `{{names}}` left in the URL produce an `Outcome.Warnings` entry (not an error).
   - Send via `httpx` with the cookie jar and OAuth2 source. An `oauth2.ErrNoToken` becomes a clear `Outcome.Err` ("No OAuth 2.0 token. Get a new token in the Auth tab.").
   - Run test scripts with the response.
   - Persist var changes (R3): `vars.Apply`, then save the environment, collection and globals **only if** they changed. Secret vars go through the SecretStore. Use a per-workspace mutex so concurrent sends don't lose writes.
   - Record history.
   - Progress steps with plain text: "Running collection pre-request script", "Resolving variables", "Waiting for response", "Running tests", "Saving variables".
   - Script errors: the send continues after a failing **test** script. A failing **pre-request** script stops the send (Postman behavior), with `Outcome.ScriptError` set.
3. **Cookie adapter:** `*httpx.CookieJar` does not implement `script.CookieAccess` (missing `ClearCookies`, maybe more). Write an adapter in engine; don't modify httpx or script. If a method needs jar support that httpx doesn't expose, add it to httpx **additively** and note it in the ledger.
4. **`pm.sendRequest`:** pass a Sender to scripts that resolves variables (using the same vars store) and uses the same jar. It doesn't record history. Recursion isn't possible because scripts don't run for sendRequest.
5. **Runner** `(e *Engine) Run(ctx, RunInput{Collection, FolderID (optional), Environment, Iterations, Data (rows from CSV/JSON, via helpers `LoadCSV`/`LoadJSON`), Delay, Bail, StopOnScriptError}, events chan<- RunEvent)`:
   - Postman runner semantics: depth-first order, `setNextRequest(name|id)` jumps within the run scope, `setNextRequest(null)` stops, local vars reset per iteration, and the data row feeds the Data scope.
   - Events: `RunStarted`, `IterationStarted`, `RequestStarted`, `RequestFinished{Outcome}`, `IterationFinished`, `RunFinished{Summary}`, all typed. The summary includes counts (requests, tests passed/failed/skipped, script errors, transport errors), total and average time, and the failure list.
   - Cancellation via ctx.
6. **History** (`internal/history`): append-only JSONL under `.postctl/history/`, bounded (default last 1,000 entries or 50 MB, compacting). Each entry has the request ID and path, a resolved method/URL/headers/body snapshot **with secrets redacted** (Authorization, Cookie, known API-key header names, any value equal to a secret variable's value → `••••`), status, time, size, test counts, and a response body up to 256 KB. API: `Append`, `List(filter)`, `Get(id)`, `Clear`.
7. **tuiengine adapter:** maps `tui.SendRequest` → `engine.SendInput`, `engine.Step` → `tui.SendStep`, and `Outcome` → `tui.SendResult` (tests, console with event names, script error text, skipped, err).

## Tests

- **Integration:** `httptest` servers plus a temp workspace (built with `store.Init` + model helpers) and `store.MemorySecrets`. Cover the full pipeline: pre-request sets an env var, it is used in the URL, the test reads the response, and the change is persisted to the env YAML and to MemorySecrets for secret vars.
- **Pipeline cases:** chain order with collection/folder/request scripts, skipRequest, a pre-request error stopping the send, a test error not stopping it, unresolved warning, OAuth2 no-token error, cookie set by server then read by `pm.cookies`, sendRequest from a pre-request.
- **Runner:** iterations with a CSV data file, `setNextRequest` jumps and stop, bail, cancellation, and event order.
- **History:** redaction (assert no secret value appears anywhere in the history files), bounds and compaction.
- **Concurrency:** race-clean concurrent sends.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green; `docs/engine.md` documents the pipeline (with a sequence diagram), the runner semantics, and the history format; ledger entries in your worktree.

## Final message

Exported API of engine, history and tuiengine; any additive changes to other packages; deviations from Postman semantics; test/lint results; commit hash.
