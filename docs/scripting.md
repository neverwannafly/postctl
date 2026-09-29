# Scripting

postctl runs Postman's pre-request and test scripts unchanged. The engine is
[goja](https://github.com/dop251/goja), a pure-Go ECMAScript runtime, so there
is no Node and no V8 to install: the `pm.*` API, the chai assertions, the
bundled npm libraries and the legacy `postman.*` globals are all compiled into
the binary.

This page is the contract. If something here says **supported**, a script
written against Postman's documentation should behave the same way; where it
says **partial** or **no-op**, the difference is spelled out.

## Running scripts

```go
func script.Run(ctx context.Context, scripts []*model.Script, in script.Input) (*script.Result, error)
```

`scripts` is the ordered chain for one event — collection, then folders
outermost first, then the request. They share one VM, so `pm.variables` and
anything a script hangs off the global object carry through, but each script
body gets its own function scope (as in Postman), and an error is attributed
to the script that raised it. Disabled and empty scripts are skipped.

```go
type Input struct {
    Event       Event            // "prerequest" | "test"
    Request     *model.Request   // backs pm.request; a pre-request script may mutate it
    Response    *httpx.Response  // backs pm.response; nil for prerequest
    Vars        vars.Store       // read and written live, so Changes() says what to persist
    Info        Info             // requestName, requestId, collectionName, iteration, iterationCount
    Sender      httpx.Sender     // performs pm.sendRequest
    SendOptions httpx.Options
    Cookies     CookieAccess     // backs pm.cookies and pm.cookies.jar(); may be nil
    Timeout     time.Duration    // whole run; zero means DefaultTimeout (5s)
    MemoryLimit int64            // heap growth cap in bytes; zero means DefaultMemoryLimit (256 MiB); <0 disables
}

type Result struct {
    Request     *model.Request   // after the scripts ran; always distinct from Input.Request
    Tests       []TestResult     // name, passed, skipped, async, error, index
    Console     []ConsoleEntry   // level, message, time
    Err         *ScriptError     // script, name, message, file:line:col, stack
    NextRequest *string          // nil = never called, "" = stop the run, else a request name
    SkipRequest bool
    TimedOut    bool
}
```

`Run` returns a Go error only when the sandbox itself cannot start
(`ErrNoRuntime`). A script that throws is reported in `Result.Err`, with the
tests and console output recorded before it failed still present, so the UI
can show partial results.

`CookieAccess` is defined by this package and satisfied by the workspace
cookie jar:

```go
type CookieAccess interface {
    Cookies(rawURL string) ([]model.Cookie, error)
    SetCookie(rawURL string, c model.Cookie) error
    UnsetCookie(rawURL, name string) error
    ClearCookies(rawURL string) error
}
```

## Compatibility table

Legend: **yes** behaves as Postman documents · **partial** works with the
stated limit · **no-op** accepted, warns on the console, does nothing ·
**no** not available.

### Variables

| API | State | Notes |
| --- | --- | --- |
| `pm.environment`, `pm.collectionVariables`, `pm.globals`, `pm.iterationData` | yes | `get` `set` `unset` `has` `clear` `toObject` `replaceIn` `variables` `syncVariablesTo` `syncVariablesFrom`. |
| `pm.variables` | yes | Reads every scope by precedence (global < collection < environment < data < local); `set` writes the local scope, as in Postman. |
| `pm.variables.replaceIn` | yes | Resolves `{{var}}` across scopes and the `{{$dynamic}}` generators. |
| `pm.<scope>.replaceIn` | partial | Substitutes from that one scope only, and does not expand `{{$dynamic}}`. |
| `<scope>.values` | partial | A rebuilt `VariableList` snapshot, not the live list: mutating it does not write back. Use `set`/`unset`. |
| Writes | yes | Go straight to the store, so the send pipeline resolves against them and they persist after the run. |

### Request

| API | State | Notes |
| --- | --- | --- |
| `pm.request.method`, `.name`, `.id` | yes | |
| `pm.request.url` | yes | `toString` `getHost` `getPath` `getPathWithQuery` `getQueryString` `getRemote` `addQueryParams` `removeQueryParams` `toJSON`; assigning a string reparses. |
| `pm.request.url.query`, `.variables` | yes | Full `PropertyList`: `add` `insert` `append` `prepend` `upsert` `remove` `get` `one` `has` `each` `map` `filter` `find` `reduce` `count` `idx` `indexOf` `clear` `populate` `repopulate` `assimilate` `toObject` `toJSON`. |
| `pm.request.url.path` | yes | A plain array of segments, as in postman-collection, so `push` and `splice` work. |
| `pm.request.headers` | yes | Same list API; lookup is case-insensitive and one key may hold several values, as in postman-collection. |
| `pm.request.body` | yes | `mode` `raw` `urlencoded` `formdata` `file` `graphql` `isEmpty()` `toString()`; edits reach `Result.Request`. |
| `pm.request.auth` | yes | `update` `use` `parameters()` `current()` `clear`. bearer, basic, apikey, digest, oauth2, awsv4, hawk and ntlm round-trip; an unmodelled scheme survives as-is. |
| `pm.request.addHeader/upsertHeader/removeHeader/getHeaders`, `.clone()`, `.size()` | yes | |
| Mutation during a test script | partial | Allowed, but the request has already been sent, so it changes nothing. Postman behaves the same way. |

### Response

| API | State | Notes |
| --- | --- | --- |
| `pm.response.code`, `.status`, `.responseTime`, `.responseSize` | yes | |
| `pm.response.text()`, `.json()` | yes | `json()` throws a `JSONError`, matching Postman, so `try`/`catch` around it works. |
| `pm.response.headers`, `.cookies` | yes | `PropertyList`s; header lookup is case-insensitive. |
| `pm.response.reason()`, `.details()`, `.size()`, `.dataURI()`, `.jsonp()`, `.contentInfo()` | yes | |
| `pm.response.to.have.status/header/body/jsonBody/jsonSchema` | yes | The real `chai-postman` plugin, so the failure messages match Postman's. |
| `pm.response.to.be.ok/success/error/clientError/serverError/redirection/accepted/...` | yes | |
| `pm.response.stream` | no | postctl keeps the body as bytes; use `text()`. |

### Tests and assertions

| API | State | Notes |
| --- | --- | --- |
| `pm.test(name, fn)` | yes | |
| `pm.test(name, done => …)` | yes | The done-callback form; `done(err)` fails the test. |
| `pm.test(name, async fn)` | yes | A returned promise is awaited. |
| `pm.test.skip(name)`, `pm.test.index()` | yes | |
| `pm.expect` | yes | chai 4.4.1 with `chai-postman`. |
| Unfinished async test | partial | Reported as a failure ("test did not finish before the script run ended") rather than hanging. |

### Execution control

| API | State | Notes |
| --- | --- | --- |
| `pm.execution.setNextRequest(name)` | yes | `null` stops the run. |
| `pm.execution.skipRequest()` | yes | Also stops the rest of the script chain. |
| `pm.execution.location` | partial | Present and iterable; `.current` is the request name. |
| `pm.execution.runRequest()` | no-op | Warns and resolves to `null`. |
| `pm.info` | yes | `eventName` `iteration` `iterationCount` `requestName` `requestId`, plus `collectionName` as a postctl extension. |

### Network and cookies

| API | State | Notes |
| --- | --- | --- |
| `pm.sendRequest(urlOrRequest, cb)` | yes | Callback form, `(err, res)`; `res` has the full `pm.response` API. |
| `pm.sendRequest(urlOrRequest)` | yes | Promise form, so `await` and `Promise.all` work. |
| `pm.cookies` | yes | `get` `one` `has` `count` `each` `toObject` over the cookies that apply to the request. |
| `pm.cookies.jar()` | yes | `get` `getAll` `set` `unset` `clear`, in both callback and promise form. |

### Libraries

`require(name)` is limited to the bundles below; anything else throws
`Cannot find module`. They load on first use and their compiled programs are
cached for the life of the process.

| Module | Version | State | Notes |
| --- | --- | --- | --- |
| `lodash` | 4.17.21 | yes | |
| `moment` | 2.30.1 | yes | Locale data is the bundled default (`en`). |
| `crypto-js` | 4.2.0 | partial | Postman ships 3.3.0. See the deviations below. |
| `uuid` | 8.3.2 | yes | Randomness comes from Go's `crypto/rand`. |
| `chai` | 4.4.1 | yes | chai 5 is ESM-only and cannot load here. |
| `chai-postman` | 2.0.1 | yes | Also what backs `pm.response.to`. |
| `ajv` | 6.12.6 | yes | |
| `tv4` | 1.3.0 | yes | |
| `xml2js` | 0.6.2 | yes | Also backs the legacy `xml2Json`. |
| `postman-collection` | 5.3.1 | partial | Available by name; its classes are not the same objects `pm.*` uses. See the deviations. |
| `buffer` | — | yes | `Buffer` is also a global. |
| Node core (`fs`, `path`, `http`, `os`, `child_process`, …) | no | There is no file system, process environment or socket in the sandbox. |

### Globals

| API | State | Notes |
| --- | --- | --- |
| `console.log/info/warn/error/debug` | yes | Values are formatted like Node's `util.inspect` (depth 4, 100 items, `[Circular]`), and a leading format string consumes `%s %d %i %f %j %o %O %c %%`. |
| `setTimeout`, `clearTimeout`, `setInterval`, `clearInterval` | yes | Driven by a real event loop; the run waits for pending timers, promises and sends, bounded by `Timeout`. |
| `atob`, `btoa`, `Buffer` | yes | |
| `crypto.getRandomValues`, `crypto.randomUUID` | yes | Backed by Go's `crypto/rand`. |
| `async`/`await`, top-level `await`, generators, classes, template literals, destructuring | yes | goja implements ES2020 or so; each script body is wrapped in an async function. |
| `fetch`, `XMLHttpRequest`, `WebSocket` | no | Use `pm.sendRequest`. |
| `pm.visualizer.set` | no-op | Warns. postctl has no HTML pane to render into. |
| `pm.vault.get/set/unset` | no-op | Warns and resolves to `undefined`. |
| `pm.require(pkg)` | no-op | Warns and returns `undefined`. Postman's private package registry has no equivalent here. |

### Legacy (pre-`pm`) globals

| API | State | Notes |
| --- | --- | --- |
| `tests["name"] = truthy` | yes | Folded into the results after the `pm.test` ones, as Postman does. |
| `responseBody`, `responseCode`, `responseTime`, `responseHeaders`, `responseCookies` | yes | `responseCode` has `code`, `name` and `detail`. |
| `request` | yes | `id` `name` `method` `url` `headers` `data`. `headers` keeps each header's original case. |
| `environment`, `globals`, `data`, `iteration` | partial | Snapshots taken before the script ran; write with `postman.set*` or `pm.*`, not by assigning into them. |
| `postman.setNextRequest` | yes | |
| `postman.get/set/clear EnvironmentVariable`, `…GlobalVariable`, `clearEnvironmentVariables`, `clearGlobalVariables` | yes | |
| `postman.getResponseHeader`, `postman.getResponseCookie` | yes | |
| `xml2Json` | yes | |
| `postman.setEnvironmentVariableFrom*`, `postman.toString` | no | Undocumented and unused in practice. |

## Deviations worth knowing

- **crypto-js is 4.2.0, not Postman's 3.3.0.** 3.x carries CVE-2023-46233, a
  weak PBKDF2 default. Only `PBKDF2` differs: it now defaults to SHA-256 with
  250 000 iterations instead of SHA-1 with 1. A script that relied on the old
  defaults must pass `{ hasher: CryptoJS.algo.SHA1, iterations: 1 }`
  explicitly. Every other digest, HMAC and cipher is unchanged.
- **`require('postman-collection')` gives you the real SDK; `pm.*` does not
  use it.** The `pm.request`, `pm.response` and `PropertyList` objects are
  postctl's own implementation of the same semantics, written against
  postman-collection's source, because loading the 834 KiB bundle costs about
  26 ms per run and the budget for a whole script is 5 ms. So
  `pm.request instanceof sdk.Request` is `false`. Everything either object
  does on its own behaves the same; only identity checks across the two
  differ.
- **Query strings are not percent-normalised.** `getQueryString()` and
  `url.toString()` return parameters exactly as written, which is what
  postman-collection does by default and what keeps `{{variable}}` references
  intact for the send pipeline to resolve.
- **`url.toString()` substitutes `:name` path variables** (matching
  postman-collection), but the URL handed back to Go keeps them unresolved,
  with their values alongside in the path variable list, because that is how
  a request is stored.
- **`<scope>.values` is a snapshot.** postctl holds no variable state of its
  own — every read and write goes to the workspace store — so there is no
  live `VariableList` to hand out. `get`/`set`/`unset`/`toObject` are the
  supported way in.
- **`pm.info.collectionName` is an addition,** not part of Postman's `pm.info`.

## Errors, timeouts and limits

A script that throws stops the chain: the scripts after it do not run, and
`Result.Err` carries the error name, message, and the file, line and column
mapped back to the script's own source rather than to the sandbox wrapper.
Syntax errors are reported the same way, before anything runs.

The whole run — including pending timers, promises and in-flight
`pm.sendRequest` calls — is bounded by `Input.Timeout`, 5 seconds by default,
which is also Postman's default. The VM is interrupted from a watchdog
goroutine, so a script with no yield point still stops: an infinite loop at
the top level, inside a timer, a promise job, an async test, a getter, a
`sort` comparator or a `replace` callback, or one that catches and ignores
the interrupt. Cancelling the context has the same effect. Either way
`Result.TimedOut` is set and `Result.Err` says the script timed out.

What the timeout can and cannot guarantee:

- **The wrap-up is bounded too.** After the script chain, postctl reads
  `pm.request` back, and that can run script-defined code (an overridden
  `pm.request.toJSON`, say). It gets its own 500 ms budget; if it overruns,
  the result is a timeout error and the request is not updated.
- **`Run` always returns.** goja checks for an interrupt between bytecode
  instructions, so a long *native* call (`new Array(4294967295).join('')`, or
  a host call that blocks) is out of the interrupt's reach. If the VM has not
  unwound 1.5 s after the deadline, `Run` abandons it and returns what the
  script had recorded by then, with `TimedOut` set. The abandoned goroutine
  cannot change anything afterwards — the bridge refuses every write to the
  variable store, the cookie jar and the result — but it keeps running until
  the native call ends, so a script can waste CPU that long. A blocking call
  already in flight at the deadline (a cookie-jar write) is not undone.
- **Regular expressions are capped per match.** Patterns Go's `regexp`
  cannot express (backreferences, lookahead) run on a backtracking engine
  inside a single native call. Every match is limited to one second, after
  which the match fails and the script carries on, so catastrophic
  backtracking costs at most a second per match instead of hanging the run.
- **Recursion is capped at 10 000 frames.** Deeper nesting stops the script
  with `RangeError: Maximum call stack size exceeded`; a script cannot catch
  it.
- **Memory is capped by heap growth.** goja has no per-VM allocation
  accounting, so `Input.MemoryLimit` (256 MiB by default; the engine's
  `Config.ScriptMemoryLimit`) is compared with the *process* heap growth since
  the run began, sampled every 10 ms. A run that exceeds it is interrupted
  with `script exceeded the memory limit of N MiB`. Two consequences: the
  check is exact for a lone script but a runaway script can also stop
  another one running at the same moment, and a single native allocation
  larger than the headroom (one huge `'x'.repeat(n)`) happens before the next
  sample and is only caught afterwards. `TimedOut` is not set for a memory
  stop.
- **`pm.sendRequest` is bounded.** At most 8 requests are on the wire at
  once and one run may make at most 256 in total; further calls fail through
  their callback (or rejected promise) with an error that says so.

A script reaches nothing outside the APIs above. There is no file system, no
process environment, no `child_process`, and the only network access is
`pm.sendRequest`, which goes through the sender the caller supplies and is
subject to the same proxy, TLS and redirect settings as a normal send.

The Go bridge (`host`) is a variable inside the sandbox's function scope, not a
global, and the bootstrap entry points (`__postctl_init`, `__postctl_run`,
`__postctl_finish`) are captured by the runner and deleted from the global
object before any user code is compiled, so a script can neither call nor
replace them. `TestSandboxHostGlobalIsNotScriptVisible` checks that
`typeof host === 'undefined'` and that no bridge function is reachable by
walking `globalThis`.

## Performance

Warm runs on an Apple M3 Pro, one script, whole `Run` including VM setup:

| Script | Time |
| --- | --- |
| Empty | 0.44 ms |
| Trivial (`pm.environment.set`) | 0.47 ms |
| HMAC signing with crypto-js | 3.4 ms |
| Five chai assertions over a JSON body | 3.7 ms |

The sandbox and every bundle are compiled once per process and shared; user
scripts are compiled once per source and cached by content hash. The first
run in a process pays an extra few milliseconds to compile the sandbox, and
the first `require` of a library pays for that library.

## Testing

`internal/script/testdata/corpus` holds the compatibility corpus: 46 cases,
each a `.js` script mirroring a real Postman idiom plus a `.json` file with
the inputs and the expected tests, console output, variable changes and
outgoing request. Regenerate the expected blocks after an intentional change
and read the diff:

```sh
go test ./internal/script -run TestCorpus -update
```

The invariant the generator cannot fake is that every case must run without a
`ScriptError` and with every test passing, unless its name says `failing`.

`TestPrivateCollections` runs the same engine over the collections extracted
from the real Postman app, if `testdata/private/staging` is present. That data
is gitignored and nothing from it is ever logged.
