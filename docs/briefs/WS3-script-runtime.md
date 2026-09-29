# WS3 — Script runtime (Postman-compatible `pm.*` on goja)

Owner model: Opus 5 Thinking (high) · Branch: `ws/3-script` · Worktree: `~/work/postctl-wt/ws3-script`

## Why this is the hardest piece

The user's main reason for leaving Postman is keeping their pre-request and test scripts working. Their real collections (17 requests with pre-request scripts so far) must run **unmodified**. Correct JS semantics, sandboxing, async `pm.sendRequest`, and faithful chai behavior all matter.

## Read first

- `docs/plan.md` §0 (binding: Q4 = tiers T1+T2, R3 variable persistence, C1 contracts), §4.1 send pipeline, §7 scripts.
- `docs/model.md` rules; `internal/model` (`Request`, `Script`, `Scripts`, `KV`, `Cookie`).
- **Contracts you consume:** `internal/vars/scope.go` (`Store`, `Scope`) and `internal/httpx/types.go` (`Sender`, `Response`, `SentRequest`). WS2 is implementing them in parallel; code against the interfaces and use in-package fakes in tests. If a contract is missing something you need, add it **additively** in your branch and document it in the ledger; the orchestrator will reconcile.
- Postman references: [Postman JavaScript reference](https://learning.postman.com/docs/tests-and-scripts/write-scripts/postman-sandbox-reference/overview/) and the open-source [postman-sandbox](https://github.com/postmanlabs/postman-sandbox) and [postman-collection](https://github.com/postmanlabs/postman-collection) (`Url`, `HeaderList`, `PropertyList` semantics) for behavior. Consult them; don't guess.

## Scope

Package `internal/script` (and `internal/script/assets/` for embedded JS bundles).

### API

```go
type Event string // "prerequest" | "test"
type Input struct {
    Event      Event
    Request    *model.Request      // resolved-for-scripts view; prerequest may mutate it
    Response   *httpx.Response     // nil for prerequest
    Vars       vars.Store
    Info       Info                // requestName, requestId, iteration, iterationCount, eventName, collectionName
    Sender     httpx.Sender        // for pm.sendRequest
    Cookies    CookieAccess        // interface you define; WS2's jar will satisfy it
    Timeout    time.Duration       // default 5s per script
}
type Result struct {
    Request    *model.Request  // possibly mutated (prerequest)
    Tests      []TestResult    // name, passed, skipped, error message
    Console    []ConsoleEntry  // level, message, timestamp
    Err        *ScriptError    // syntax/runtime error with file:line mapped to Script.File
    NextRequest *string        // pm.execution.setNextRequest / postman.setNextRequest (nil = unset, "" = stop)
    SkipRequest bool           // pm.execution.skipRequest()
}
func Run(ctx context.Context, scripts []*model.Script, in Input) (*Result, error)
```

`scripts` is the ordered chain (collection → folders → request) for one event, run in **one** shared VM so `pm.variables` etc. carry through, but with separate error attribution per script. Adjust the exact types if needed, but keep them this simple and document them in `docs/scripting.md`.

### T1 + T2 surface (must pass the compat corpus)

`pm.environment`, `pm.collectionVariables`, `pm.globals`, `pm.variables` (`get/set/unset/has/clear/toObject/replaceIn`; `pm.variables.get` resolves across scopes), `pm.iterationData`, `pm.request` (`url` with `toString/getHost/getPath/getQueryString`, `query`/`headers` as PropertyList with `add/upsert/remove/get/has/each/toObject`, `method`, `body` (mode/raw/urlencoded/formdata), `auth`; mutable in prerequest and reflected in `Result.Request`), `pm.response` (`code`, `status`, `headers`, `responseTime`, `responseSize`, `text()`, `json()`, `to.have.status/header/body/jsonBody`, `to.be.ok/success/error/clientError/serverError`), `pm.test` (sync, async via returned promise/`done`, `pm.test.skip`), `pm.expect` (real chai), `pm.info`, `pm.cookies` (`get/has/toObject`, `pm.cookies.jar()` with `get/set/unset/clear` callbacks), `pm.sendRequest` (string URL or request object, callback `(err, res)` **and** promise form; `res` has the pm.response API), `pm.execution.skipRequest/setNextRequest`, legacy `postman.setNextRequest`, `console.log/info/warn/error/debug` (format objects like Node's inspect, briefly), `require()` for `lodash`, `moment`, `crypto-js`, `uuid`, `chai`, `ajv`, `xml2js`, `atob`/`btoa`, `setTimeout`/`clearTimeout`/`setInterval` driven by an event loop (`goja_nodejs/eventloop`). Script completion waits for pending timers, promises and sendRequests, bounded by `Timeout`.

T3 (cheap wins only): legacy `tests["name"] = bool`, `responseBody`, `responseCode`, `responseHeaders`, `responseTime`, `postman.get/setEnvironmentVariable`, `postman.get/setGlobalVariable`. `pm.visualizer.set`, `pm.vault.*` and `pm.require` must no-op with a console warning, not crash.

### Engine and bundles

- `github.com/dop251/goja` + `github.com/dop251/goja_nodejs` (require registry, console, eventloop, buffer, url). Pure Go.
- Vendor the npm libraries as pinned, minified UMD/CommonJS bundles embedded via `embed` under `internal/script/assets/`, with a `README.md` recording versions, sources and licenses, plus a `task assets` script to rebuild them. Check each bundle actually loads in goja (ES5/ES2015 target; transpile or pick older builds if needed). Load bundles lazily on first `require` and cache compiled `*goja.Program`s across runs.
- Sandbox: no host access except the APIs above. Timeouts use `vm.Interrupt`. Guard infinite loops. Recover panics into `ScriptError`. Map stack traces to `Script.File` and line.
- Performance: a warm run of a trivial script should take under 5 ms. Pre-compile per script and cache by content hash.

## Compat corpus

`internal/script/testdata/corpus/`: at least 40 synthetic scripts that mirror real Postman idioms (auth token chaining, HMAC signing with crypto-js, timestamps with moment, schema checks with ajv/tv4-style, `pm.sendRequest` login-then-set-token, `setNextRequest` flows, chai chains like `pm.expect(x).to.be.an('array').that.is.not.empty`, `pm.response.to.have.jsonBody('a.b')`). Each case records expected tests, var changes and console output. Also add a private-data test: if `testdata/private/staging/` exists (copy it from `/Users/shubham/work/postctl/testdata/private/staging`, it's gitignored), load every collection JSON, run every pre-request script against fake vars/sender, and assert **no ScriptError** from unsupported APIs. Report the success rate in the ledger as counts only. Never print variable values or script contents from private data.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green; corpus green; private scripts: 100 % of the 17 real pre-request scripts run without unsupported-API errors (any that can't are listed by request name with a reason); `docs/scripting.md` documents the supported surface as a compatibility table (supported / partial / no-op / unsupported). Ledger entries in your worktree.

## Parallel agents

WS0.1 (model/store), WS1 (importer), WS2 (vars/httpx; implements your contracts), WS2b (oauth2), WS5 (TUI). Only create files under `internal/script/` and `docs/scripting.md`, plus additive contract edits.

## Final message

Exported API, compat table summary, private-script success rate (counts), contract additions, deviations, perf numbers, test/lint results, commit hash.
