# Flow: running a Postman script

```mermaid
sequenceDiagram
  participant Engine
  participant Run as script.Run
  participant Loop as goja event loop
  participant Boot as embedded bootstrap
  participant Bridge as Go bridge
  participant HTTP as pm.sendRequest sender
  Engine->>Run: scripts + Input
  Run->>Loop: VM, timeout, watchdog
  Loop->>Boot: initialize pm and globals
  Boot->>Bridge: context and host callbacks
  Loop->>Loop: compile/run user async functions
  Loop->>HTTP: pm.sendRequest (optional)
  Loop->>Loop: drain timers/promises/tests
  Loop->>Bridge: finish and request snapshot
  Run-->>Engine: Result
```

1. `internal/script/script.go` `Run` normalizes the event and limits, filters
   disabled/empty scripts, and calls the private runner. Its `Input` carries
   the unresolved or resolved `model.Request`, optional `httpx.Response`, live
   `vars.Store`, sender, cookie access, request metadata, timeout, and memory
   limit. Script failures are `Result.Err`; only runtime bootstrap failure is a
   Go error.

2. `internal/script/run.go` `run` loads the cached bootstrap program, creates a
   `goja_nodejs/eventloop`, and builds `bridge`. It derives a timeout context,
   gives the runtime a 10,000-frame call-stack cap, and starts execution in a
   goroutine so a native call cannot block the caller forever.

3. `internal/script/run.go` `start` executes the embedded bootstrap, captures
   and deletes `__postctl_init`, `__postctl_run`, and `__postctl_finish`, then
   creates the bridge object. The bootstrap defines `pm`, Postman legacy
   globals, chai, console, Buffer, crypto, timers, and restricted `require`.
   The Go host object remains in function scope and is not reachable through
   `globalThis`.

4. Each source is wrapped as its own async function by the program cache,
   compiled with its real `.js` filename, and passed to the bootstrap in order.
   One `Run` call can share globals across a chain; the engine intentionally
   calls it once per collection/folder/request level so progress and errors
   have a precise origin.

5. `internal/script/bridge.go` `bridge.object` exposes narrow callbacks for
   variable scopes, request/response conversion, tests, console, cookies,
   execution control, libraries and network. To add a `pm.*` API:
   - define the JS-facing behavior in the embedded bootstrap;
   - add the smallest host callback and data shape in
     `internal/script/bridge.go` only if Go state is needed;
   - add a compatibility corpus case under
     `internal/script/testdata/corpus`;
   - document support/deviation in `docs/scripting.md`.
   Keep filesystem, process environment, arbitrary modules, and direct sockets
   unreachable.

6. `pm.sendRequest` reaches `bridge.sendRequest`, which parses a request,
   calls the injected `httpx.Sender`, and schedules callback/promise completion
   on the event loop. Engine injects a sender that resolves against the live
   variable store and shares HTTP options/cookies. It does not run scripts or
   write history. A run allows 8 in flight and 256 total.

7. The event loop waits for timers, promises, callback sends and async tests.
   `setTimeout`/`setInterval` are real event-loop tasks; no busy waiting is
   needed. `pm.test` records pass/fail/skip, console calls append structured
   entries, and `setNextRequest`/`skipRequest` set fields on `Result`.

8. `internal/script/run.go` `watch` interrupts bytecode on timeout,
   cancellation, or heap-growth limit. The default whole-run timeout is 5s and
   default memory growth cap is 256 MiB. Backtracking regex matches have a 1s
   cap; recursion is 10,000 frames. If a native call ignores interruption,
   `run` abandons the VM after 1.5s, freezes bridge writes, and returns the
   partial result. Wrap-up has a separate 500ms budget.

9. `internal/script/run.go` `finish` maps errors back to original file/line,
   settles unfinished async tests and legacy `tests`, and reads a mutated
   pre-request back into `Result.Request`. The engine folds `Tests`, `Console`,
   `NextRequest`, `SkipRequest`, timeout, and error into its outcome.

Secret values enter only through the hydrated live variable store or resolved
request. The runtime keeps them in memory; it does not mask values internally
because scripts must operate on plaintext. Engine/TUI/CLI/report boundaries
mask console, assertion and error text with the outcome `history.Redactor`.
