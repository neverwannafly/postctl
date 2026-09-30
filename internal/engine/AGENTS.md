# internal/engine — local rules

Read the root [`AGENTS.md`](../../AGENTS.md) and [`docs/engine.md`](../../docs/engine.md) (pipeline, precedence, persistence, runner, deviations) first. The engine is the one place that knows the order of operations. The TUI (through `internal/tui/tuiengine`) and the CLI (`internal/cli`) are thin clients.

## Invariants

- **No presentation imports.** `internal/engine` must never import `internal/tui`, `internal/ui` or any Charm module. Return typed values (`Outcome`, `RunEvent`, `Step`); the client renders them.
- **Never mutate the caller's models.** `Send` and the runner take `Clone()` copies of collection, environment and globals and hydrate secrets on the copies (`TestSendDoesNotMutateCallerModels`).
- **Order is the contract**: scope chain; pre-request scripts (collection, folders outermost first, request; one `script.Run` per level, request unresolved); resolve (`vars.ResolveRequest`); send (`httpx`, cookie jar, stored OAuth2 token, never fetched here); test scripts; persist; history. Changing a step means updating the diagram and step list in `docs/engine.md`, the `tuiengine` adapter, the CLI reporters and the tests.
- **Precedence**: global < collection < folder < environment < data < local. Folder variables are installed per request in `session.execute` (a run crossing folders resolves each request with its own chain). `RunInput.Locals` and `SendInput.Locals` (`--var`) are re-seeded per iteration and beat everything.
- **Persistence is narrow.** After the tests, only changed keys go to `Update{Environment,Collection,Globals}Variables` (`persist.go`), which re-read under the store lock. Only secrets a script changed are re-written to the `SecretStore`. A save failure is a warning, not an error. `RunInput.NoPersist` keeps changes in memory for the rest of the run.
- **Masking.** `Outcome.Redactor` (from `session.refreshSecrets` and `session.addAuthSecrets`) knows every hydrated secret: environment, collection, folder and globals secret variables, script-written secrets, OAuth token strings. Anything that leaves the engine for display or disk (history, reports, console, test messages, script errors, transport errors) must be masked with it. `Outcome.Resolved` can contain secrets; never log it or put it in an error. Transport errors redact the URL Go embeds.
- **Errors**: expected failures live in `Outcome.Err` and `Outcome.ScriptError`; `Send` returns a Go error only for unusable input (`ErrInvalidInput`). HTTP 4xx and 5xx are responses. A failing pre-request script stops that send (no history); a failing test script keeps the response and variables.
- **Runner**: `RunFinished` is always delivered, also after cancel; `Run` never closes the channel, `RunAsync` does. WebSocket requests are listed as skipped ("WebSocket requests are not run"), never counted as failures. Anything new that iterates requests must skip `model.KindWebSocket`.
- Safe for concurrent use: any number of sends and runs may overlap (`-race`).

## Key files

| File | Role |
| --- | --- |
| `engine.go`, `types.go` | `Config`, `Engine`, `Outcome`, `Step`, `RunInput`, events |
| `send.go`, `session.go` | `Send`; `session` holds one send or run (vars store, secrets, chain, `execute`) |
| `run.go`, `data.go` | runner, `setNextRequest`, CSV and JSON data files |
| `persist.go`, `record.go` | variable write-back, history entry building |
| `cookies.go` | `NewCookieAccess` adapter from `httpx.CookieJar` to `script.CookieAccess` |

## Tests

```sh
go test -race ./internal/engine                  # ~6 s
go test -race -count=1 ./internal/engine ./internal/qa
```

- Helpers in `helpers_test.go`: `newServer` (httptest), `newFixture` (temp workspace via `store.Init` and `store.MemorySecrets`). Use synthetic data only.
- Cover cancellation, timeouts and masking for any new step. Persistence changes need a concurrent-writer test (`persist_concurrent_test.go`, `persist_stale_test.go`).
- Any new data that leaves the engine goes into the `internal/qa` leak sweep.

## Pitfalls

- Folder variables were once missing from send while the TUI preview showed them; keep preview, curl and send on the same helpers.
- Keychain hydration per send is slow with many secrets: rely on the store's read cache and only hydrate what the chain needs.
- Skipped requests run no tests, write no history, but their variable writes still persist.
- Scripts share variables across levels, not JS globals; do not build features that assume otherwise.
