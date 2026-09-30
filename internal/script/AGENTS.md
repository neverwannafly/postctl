# internal/script — local rules

Read the root [`AGENTS.md`](../../AGENTS.md) and [`docs/scripting.md`](../../docs/scripting.md) (the compatibility contract) first. This package runs Postman pre-request and test scripts on goja (pure Go) with a Postman-compatible `pm.*` API. Scripts come from imported, possibly hostile or buggy collections.

## Layout

| Path | Role |
| --- | --- |
| `script.go` | `Run`, `Input`, `Result`, `CookieAccess` (public contract) |
| `run.go`, `limits.go` | VM lifecycle, event loop, watchdog, timeouts, memory cap, regexp timeout, recursion cap, abandonment, `takeEntryPoints` |
| `bridge.go` | the Go functions the sandbox can call (variables, console, tests, `sendRequest`, cookies) |
| `convert.go`, `programs.go` | Go/JS data conversion (Postman v2.1 definitions cross as JSON strings); compiled-program caches |
| `assets/sandbox/NN-*.js` | the `pm.*` implementation in JavaScript; files are concatenated **in filename order** inside one IIFE, so the prefix is load order |
| `assets/bundles/*.js` | pinned, committed npm bundles (`lodash`, `moment`, `crypto-js`, `uuid`, `chai`, `ajv`, ...); only `task assets` rebuilds them (node/npm, see `assets/README.md`) |
| `testdata/corpus/` | compatibility corpus: `NN-name.js` plus `NN-name.json` (inputs and expected results) |

## Invariants

- **Bounded, always.** Default timeout 5 s (`DefaultTimeout`); a watchdog interrupts the VM and context cancellation does the same. `Run` returns at most 1.5 s past the deadline even if a native call is stuck (it abandons the VM; the bridge then refuses every write). The wrap-up after the chain has a 500 ms budget. Regexp matches cap at 1 s, call depth at 10 000, heap growth at `DefaultMemoryLimit` (256 MiB), `pm.sendRequest` at 8 in flight and 256 per run. Do not loosen these; if you add a host call that can block or allocate a lot, bound it and add a test like the ones in `limits_test.go` (`TestInfiniteLoopVariantsTerminate`).
- **No new globals, no new host surface.** A script sees `pm`, `console`, timers, `atob`/`btoa`, `Buffer`, `crypto`, the legacy Postman globals and `require` of the bundled allowlist. Nothing else: no `fs`, `process`, `fetch`, no Go bridge. `host` is a local of the sandbox function, and `__postctl_init`, `__postctl_run` and `__postctl_finish` are captured and deleted before user code compiles. A new bridge function must be added to `bridgeFunctionNames` in `sandbox_globals_test.go`, which must still pass (`TestSandboxHostGlobalIsNotScriptVisible`).
- **No secret output.** Console lines, test messages and errors can contain secret values (scripts `console.log` them). This package records them as is; the engine and every presenter must mask them (`history.Redactor`). Never log script values yourself.
- `Result.Request` is always a distinct value from `Input.Request`; the pipeline diffs them. Variable reads and writes go live to `vars.Store`; `Changes()` tells the engine what to persist.
- One `script.Run` per chain level (collection, each folder, request); levels share the `vars.Store`, not JS globals.
- Async matters: `pm.sendRequest` keeps the loop alive with a JavaScript timer (`eventloop.SetTimeout` increments the job count asynchronously and lets the loop exit early); `Terminate` leaves the job count inflated, so finish by talking to the runtime directly. Unfinished async tests are reported as failures.
- Data crossing Go to JS must be deterministic: a Go map reached JS once (`toObject()`) and changed key order per run; pass JSON strings or ordered structures.

## Changing the `pm.*` surface

1. Implement it in the right `assets/sandbox/NN-*.js` (edit the JS; no rebuild step needed, it is embedded).
2. Add a unit test in this package (`newHarness(EventTest)` style) and, for any Postman idiom, a corpus case; then `go test ./internal/script -run TestCorpus -update` and read the diff. The corpus invariant: every case runs with no `ScriptError` and all tests passing unless its name says `failing`.
3. Update the compatibility table and deviations in `docs/scripting.md`; if engine data is needed, add a field to `Input` and wire it in `internal/engine`.
4. Deviations from Postman (crypto-js 4.2.0, `pm.*` not instances of postman-collection classes, query strings not normalized) are documented, not hidden.

## Tests

```sh
go test -race ./internal/script                          # ~20 s
go test ./internal/script -run TestCorpus -update        # regenerate expected blocks
go test ./internal/script -run TestPrivateCollections -v # skips unless testdata/private/staging exists
```

`TestPrivateCollections` runs real extracted scripts; it logs counts only. Keep it that way.
