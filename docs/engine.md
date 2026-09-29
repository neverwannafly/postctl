# Engine

`internal/engine` is the one place that knows the order of operations. The TUI
(`internal/tui/tuiengine`) and the CLI are thin clients over it. It imports no
TUI or Charm package (AGENTS.md architecture rules).

```go
eng, err := engine.New(engine.Config{Store: st}) // everything else has a default

out, err := eng.Send(ctx, engine.SendInput{Collection: c, Request: r, Environment: env}, progress)
sum, err := eng.Run(ctx, engine.RunInput{Collection: c, Environment: env, Iterations: 3}, events)
```

`Config` fields: `Store` (required), `Secrets` (default: the store's),
`HTTP` (`httpx.NewClient(Version)`), `Cookies` (`httpx.NewCookieJar(root)`),
`OAuth2` (an `oauth2.Source` over `Secrets`), `History` (a log under
`.postctl/history`; `DisableHistory` turns it off), `Version`, `Clock`,
`ScriptTimeout`, `MaxBodyBytes`, `Defaults` (workspace request settings).

An `Engine` is safe for concurrent use. Any number of sends and runs may
overlap; see [Persistence](#persistence).

## Send pipeline

`Send` implements plan §4.1. It never mutates a caller's model: at the start it
takes deep copies (`Clone`) of the collection, environment and globals and
hydrates their secret variables on the copies.

```mermaid
sequenceDiagram
    participant C as Caller (TUI / CLI)
    participant E as engine.Send
    participant S as script.Run
    participant V as vars
    participant H as httpx
    participant D as store + history

    C->>E: SendInput, progress
    E->>D: hydrate secret variables (on clones)
    E->>E: build vars store (global < collection < folder < env < data < local)
    loop collection, each folder, request
        E-->>C: "Running … pre-request script"
        E->>S: Run(prerequest, unresolved request)
        S-->>E: request', tests, console, skip / next / error
        Note over E: error stops the send; skipRequest ends it (no history)
    end
    E-->>C: "Resolving variables"
    E->>V: ResolveRequest(request', chain, store)
    E-->>C: "Waiting for response"
    E->>H: Send(resolved, cookie jar, OAuth 2.0 source)
    H-->>E: Response, or transport / no-token error
    loop collection, each folder, request
        E-->>C: "Running … tests"
        E->>S: Run(test, resolved request, response)
        Note over E: an error keeps response, tests so far and variables
    end
    E-->>C: "Saving variables" (only if something changed)
    E->>D: SaveEnvironment / SaveCollectionVariables / SaveGlobals
    E->>D: history.Append (redacted)
    E-->>C: Outcome
```

Details that matter:

- **Pre-request scripts see the request unresolved**: `pm.request.url` is
  `{{baseUrl}}/x`, not the substituted URL. The request is resolved after the
  scripts ran, from the request the scripts left behind (`Result.Request`).
- **One `script.Run` per level.** Collection → folders (outermost first) →
  request, for both events. The levels share the live variable store, and each
  level's `pm.request` is the previous level's result. Running them separately
  lets the progress text name the level.
- **Resolution** is `vars.ResolveRequest`, which also flattens inherited auth.
  `{{names}}` still in the URL afterwards become `Outcome.Warnings`
  ("Unresolved variable {{x}} in the URL"), never an error.
- **OAuth 2.0.** The engine reads the stored token through `oauth2.Source` and
  never fetches one. With none, `Outcome.Err` is an `*OAuthTokenMissingError`
  whose message is "No OAuth 2.0 token. Get a new token in the Auth tab." and
  which unwraps to `oauth2.ErrNoToken`.
- **Errors.** A transport error redacts the URL Go embeds in it (query
  credentials). Cancellation is `Outcome.Err == context.Canceled`.
  `Send` returns an error only for unusable input (`ErrInvalidInput`).
- **Skipped requests** (`pm.execution.skipRequest()`): no send, no test
  scripts, no history entry; variables set so far still persist.
- **Progress steps** (`Step.Text`): "Running collection pre-request script",
  "Running folder pre-request script", "Running pre-request script",
  "Resolving variables", "Waiting for response", "Running collection tests",
  "Running folder tests", "Running tests", "Saving variables". `Step.Kind` is
  one of `prerequest`, `resolve`, `send`, `test`, `save`. Steps for levels
  without a script are not emitted.

### Outcome

| Field | Meaning |
| --- | --- |
| `Response` | nil when skipped or the transport failed |
| `Resolved` | the request handed to httpx; **can contain secrets, never log it** |
| `Tests`, `TestCounts()` | in execution order, with `Origin` (collection/folder/request) |
| `Console` | combined, with the event (`prerequest`/`test`) |
| `ScriptError` | the failure that ended the pipeline (pre-request) or was recorded (test): event, origin, text, `TimedOut` |
| `Skipped`, `NextRequest` | from `skipRequest` / `setNextRequest` (`""` means `null`) |
| `Warnings` | unresolved variables, missing secrets, a failed save or history write; never secret values |
| `Persisted` | scopes written: `environment`, `collection`, `globals` |
| `HistoryID`, `Err`, `Duration`, `Path` | |

`Outcome.Failed()` is true for a transport error, a script error or a failing
test.

## Variable precedence

From lowest to highest, a `{{name}}` resolves to the first scope below that
defines it: **global < collection < folder < environment < data < local**
(`pm.variables.set`, `--var`).

`--var k=v` on `postctl send` and `postctl run` is the **local** layer
(`SendInput.Locals`, `RunInput.Locals`), the same layer as `pm.variables.set`,
so it beats folder, collection, environment and iteration-data values of the
same name. On `run` it is re-seeded at the start of every iteration.
`TestLocalVarsBeatFolderVariables` pins this. `--var` values are not written to
disk.

Folder variables (`folder.yaml`, Postman v2.1 `item[].variable` on a folder) are
part of the collection tier, as in Postman: they beat collection variables and
lose to the environment. With nested folders the inner one wins. Only the
folders above the request being sent apply, and disabled variables are
ignored.

There is one resolution path. `vars.FolderVariables(chain)` collects the
folders' variables and `MemStore.SetFolderVariables` installs them on the
variable store; the engine does this per request in `execute` (so a run
crossing folders resolves each request with its own chain), `postctl curl`
does it before resolving, and the TUI preview and copy-as-curl build the same
layering (`TestScopeMatchesEngineFolderPrecedence` keeps them equal). Secret
folder variables are hydrated from the `SecretStore` and masked in history like
any other secret.

Folder variables are read-only for scripts. `pm.variables.get`, `pm.variables.has`
and `{{name}}` see them; `pm.collectionVariables.get`, `.has`, `toObject()` and
`pm.variables.toObject()` do not list them. `pm.collectionVariables.set(k, v)`
writes the collection's own variable `k`, which a folder variable of the same
name still shadows for requests below that folder. A send never writes a folder
variable back to disk.

## Persistence

Plan §0 R3: `pm.environment`, `pm.collectionVariables` and `pm.globals` writes
persist; `pm.variables` (Local) does not.

After the test scripts, the engine applies the changes (`vars.Apply`) and saves
the environment, the collection and the globals **only for scopes that changed
and only if the resulting variables differ**. A save failure is a warning; the
values stay live for the rest of a run.

- **Secrets.** A write to an existing secret variable keeps its type and ref;
  the store moves the value to the `SecretStore`. Only secrets a script
  changed are handed to the store with a value, so an untouched secret is saved
  by reference and never re-written to the Keychain.
- **Concurrency.** The engine never writes back the snapshot it took when the
  send started. Each persist hands the store only the keys the send changed
  (`UpdateEnvironmentVariables`, `UpdateCollectionVariables`,
  `UpdateGlobalsVariables`); the store re-reads the file under the workspace
  write lock, applies those keys, and writes it atomically (temp file and
  rename), keeping everything else as it is on disk: other variables, the
  environment name and `production` flag, and every secret it did not touch.
  The lock is a per-directory mutex plus an advisory `flock` on
  `.postctl/store.lock`, and every store writer takes it (the TUI's saves, the
  CLI's `env set`, imports, the engine), so a TUI edit, a send and a
  `postctl run` in another terminal never drop each other's variables. Writes
  to the same key are last-writer-wins.
- **No environment selected.** An environment write cannot be saved; the send
  gets a warning.
- **Globals** come from `SendInput.Globals` or, when nil, from `globals.yaml`
  (cached by size and mtime). A missing file is created on the first write.

## Cookies and `pm.sendRequest`

- `engine.NewCookieAccess(jar)` adapts `*httpx.CookieJar` to
  `script.CookieAccess`, built only on the jar's public API. `Cookies` returns
  what the jar would send to a URL, with domain, path, expiry and flags from
  the jar's records. `SetCookie` stores as if the URL had sent it.
  `UnsetCookie(url, name)` and `ClearCookies(url)` remove every cookie whose
  domain matches the URL's host (any path). Scheme-less URLs are accepted.
- `pm.sendRequest` goes through a sender that resolves `{{variables}}` from the
  live store and uses the same jar, OAuth 2.0 source and defaults. It never
  runs scripts and never enters history.

## Runner

`Run(ctx, RunInput, events)` follows the Postman runner.

- **Order.** Depth first, in display order, once per iteration. `FolderID`
  limits the scope; collection and ancestor-folder scripts and auth still apply.
- **Iterations.** `Iterations`, else the number of data rows, else one. When
  `Iterations` exceeds the rows they are reused in order (row `n mod rows`).
- **Variables.** One variable store lives for the whole run, so environment,
  collection and global writes are visible to later requests and persisted as
  they happen unless `RunInput.NoPersist` is set (changes stay in memory for
  the rest of the run but are not written to disk). `pm.variables` (Local) is
  cleared at the start of every iteration (then re-seeded from
  `RunInput.Locals`), and the iteration's data row becomes the Data scope
  (`pm.iterationData`).
- **`OnlyRequests`.** When non-empty, only those request IDs run, in their
  usual depth-first order within the scope. An empty slice runs every request
  in the scope.
- **`setNextRequest(name | id)`** jumps to the first request of the run scope
  with that name (names win over IDs) and continues in order from there, so it
  can loop. `setNextRequest(null)` stops the whole run (`StopNextNull`). A
  target outside the scope, or outside `OnlyRequests` when that filter is set,
  stops the run and counts as a script error (`StopNextMissing`).
- **Failures.** A failing pre-request script skips that request only. `Bail`
  stops after the first request with a failing test, script error or transport
  error (`StopBail`); `StopOnScriptError` stops after a script error
  (`StopScriptError`).
- **Delay** is the pause between requests (not before the first).
- **Cancellation.** The context ends the run (`StopCancelled`); the in-flight
  request is cancelled and is not counted as a transport error.
- **WebSocket requests.** Runs list them as skipped with the reason
  "WebSocket requests are not run". They do not execute scripts or count as
  requests, failures or transport errors.

### Events

`events` (may be nil) receives typed values in this order:

```text
RunStarted
  IterationStarted
    RequestStarted
      RequestProgress*      one per pipeline step
    RequestFinished{Outcome}
  IterationFinished
RunFinished{Summary}
```

`Run` never closes the channel. The consumer must keep receiving until it sees
`RunFinished`, which is always delivered, also after cancellation; other events
may be dropped after cancellation if the consumer is slow. `RunAsync` is the
convenient form: it returns a buffered channel that is closed after
`RunFinished`.

### Summary

`RunSummary` has `Started`, `TotalTime` (wall time, delays included),
`AvgResponseTime`, `Iterations`, `Requests` (attempted, skipped excluded),
`Skipped`, `Tests{Passed,Failed,Skipped}`, `ScriptErrors`, `TransportErrors`,
`Failures` (kind `test`, `script` or `transport`, with iteration, request, path,
test name and message) and `StopReason`. `ExitCode()` follows plan §9: 2 for
transport errors, else 1 for failing tests or script errors, else 0.

### Data files

`LoadCSV(r)` (first row is the header; values are strings; a BOM is ignored;
ragged rows are an error), `LoadJSON(r)` (an array of objects, or one object;
JSON types are kept; integers beyond 2^53 become strings), and
`LoadDataFile(path)` choosing by `.csv` / `.json`.

## History

`internal/history` keeps `.postctl/history/history.jsonl`: one JSON object per
line, append-only, with a sibling `history.lock` used as an advisory `flock` so
the TUI and the CLI can share it.

```json
{"id":"…","time":"…","requestId":"…","requestName":"…","path":["Collection","Folder","Request"],
 "collectionId":"…","environment":"Staging",
 "request":{"method":"POST","url":"https://…?token=••••","headers":[{"key":"Authorization","value":"••••"}],"body":"…"},
 "status":201,"statusText":"Created","durationMs":184,"sizeBytes":1234,
 "tests":{"passed":3,"failed":0,"skipped":0},"scriptError":"","error":"",
 "response":{"headers":[…],"body":"…","bodyEncoding":"","truncated":false}}
```

- **API.** `Open(dir, Options)`, `Append(Entry)`, `List(Filter)` (newest first,
  summaries without bodies; filter by request, collection, text, time, limit and
  offset), `Get(id)` (full entry), `Clear()`, `Len()`.
- **What is recorded.** The request as it went on the wire (`Response.Sent`),
  or the resolved request when the transport failed. Skipped requests, failed
  pre-request scripts and cancelled sends are not recorded.
- **Redaction** (`history.Redactor`), before anything is written:
  header or query/form/JSON field names that look like credentials
  (Authorization, Cookie, Set-Cookie, `*token*`, `*secret*`, `*password*`,
  `*apikey*`, `key`, and the name configured for API-key auth) get `••••`; the
  password in URL user info is masked; every occurrence of a secret variable's
  value (including values a script just wrote) is replaced, in URLs, headers
  and bodies. Secrets of three characters or fewer are only masked when they are
  a whole value, since replacing them inside text would wreck it. Binary
  bodies are stored base64 (`bodyEncoding`).
- **Bounds.** Bodies are capped at 256 KB. The log keeps the newest 1,000
  entries and 50 MB. Exceeding either bound (entries by more than 10%)
  compacts the file by rewriting the newest entries within 1,000 entries and 90%
  of the byte bound through a temp file and a rename, so it does not rewrite on
  every append.
- Cookie values live in `.postctl/cookies.json` (httpx's jar, mode 0600), not in
  history.

## TUI adapter

`tuiengine.New(eng)` implements `tui.Sender`:

| `tui` | from `engine` |
| --- | --- |
| `SendRequest{Collection, Request, Environment}` | `SendInput` |
| `SendStep{Text}` | `Step.Text` |
| `SendResult.Tests` | `Outcome.Tests` (name, passed, skipped, message) |
| `SendResult.Console` | `Outcome.Console` with its event |
| `SendResult.Warnings` | `Outcome.Warnings`; the response pane labels them above the body |
| `SendResult.ScriptError` | "Pre-request script failed: …", "Test script (collection) failed: …", "… timed out: …" |
| `SendResult.Skipped`, `.Err`, `.Response`, `.Started`, `.RequestID` | same fields |

## Deviations from Postman

- Each script level runs in its own sandbox; anything a script hangs off the
  global object does not carry to the next level (variables do).
- A failing test script stops the remaining test levels (Postman keeps running
  the others).
- `setNextRequest` to an unknown request stops the run with a script error.
- Iteration data rows are reused when `Iterations` exceeds the row count.
- Warnings, history and secret handling as described above have no Postman
  equivalent.

## Tests

`internal/engine` integration tests use `httptest` servers, a temp workspace
from `store.Init` and `store.MemorySecrets`: the full pipeline and its
persistence, chain order, skip, pre-request and test errors, warnings, OAuth 2.0,
cookies, `pm.sendRequest`, cancellation, timeouts, globals, the runner (CSV
data, jumps, loops, bail, cancellation, events, delay), concurrent sends and
runs under `-race`, and history redaction over the files on disk.
`internal/history` covers redaction, bounds, compaction and shared handles.
