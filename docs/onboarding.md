# First hour in postctl

The goal is to see one request cross every layer, then make one contained
change without learning the whole codebase at once.

## 0–15 minutes: orient

Read in this order:

1. `README.md` for the product and user surface.
2. `docs/plan.md` §0 for binding decisions.
3. `docs/architecture.md` for layers, contracts, concurrency and disk state.
4. `docs/flows/startup.md` and `docs/flows/send-request.md` for the main path.
5. `docs/model.md` for the persisted domain and `docs/design-language.md` if
   your change is visible.
6. The one focused flow relevant to your task; use `docs/ledger.md` only to
   understand why an odd choice exists.

Keep this invariant in mind: model values flow inward to the engine and lower
services; typed outcomes flow outward. Presentation never owns HTTP, script,
storage, or Keychain policy.

## 15–30 minutes: run it

```sh
go version
go build -o ./bin/postctl ./cmd/postctl

tmp="$(mktemp -d)"
cp -R examples/httpbin/. "$tmp/"
./bin/postctl ls --workspace "$tmp"
POSTCTL_FAKE_SENDER=1 ./bin/postctl --workspace "$tmp"
```

In the fake TUI, open a request and press Ctrl-S. Resize across 140 and 100
columns, open `?` and Ctrl-P, then edit a YAML or sibling `.js` file in the
temporary workspace and watch it reload.

Next run focused tests and the full fast test set:

```sh
go test ./internal/engine ./internal/tui ./internal/store
go test ./...
```

## 30–50 minutes: worked first contribution

Exercise: add `postctl ls --ids`, showing each node's stable ID in human text
output. JSON already contains IDs, so this change stays at the CLI edge and
must not touch the model, store, or engine.

1. Start at `internal/cli/ls.go` `runLs`. Add a boolean flag beside `--flat`
   and `--json`, then pass it into the text writer. Preserve current output
   when false and do not change `lsItem`, because machine output already has
   the field.

2. Change the smallest rendering seam, `internal/cli/ls.go` `writeTree`, to
   append a clearly delimited ID when the option is true. IDs are not secrets,
   but names and paths still pass through existing output handling.

3. Add table cases in `internal/cli/cli_test.go` covering:
   default output unchanged; `--ids` at the root; `--ids` with `--flat`; and
   `--json --ids` either rejected as redundant or producing unchanged JSON
   (choose and document one behavior).

4. Update the command usage table in `internal/cli/cli.go` `commands`, the
   matching table in `cmd/postctl/main.go` `commands`, and `docs/cli.md`.
   Search for the exact old usage string so help cannot drift.

5. Run:

   ```sh
   gofmt -w internal/cli/ls.go internal/cli/cli_test.go
   go test ./internal/cli ./cmd/postctl
   go build ./...
   go test -race ./...
   golangci-lint run
   ```

This pattern is representative: parse at an edge, preserve a stable lower
contract, test both default and opt-in behavior, update generated/user-facing
references, then gate. For a first `pm.*` contribution, follow
`docs/flows/scripts.md` instead and begin with a corpus case.

## 50–60 minutes: follow one real call chain

Put breakpoints or temporary test logs (never secret values) on:

- `cmd/postctl/main.go` `run`
- `internal/cli/send.go` `runSend` or
  `internal/tui/request/sendflow.go` `Pane.sendNow`
- `internal/engine/send.go` `Engine.Send`
- `internal/engine/session.go` `session.execute`
- `internal/vars/resolve.go` `ResolveRequest`
- `internal/httpx/client.go` `Client.Send`
- `internal/engine/record.go` `session.record`

Remove temporary logging before committing. At that point you have seen the
same architecture used by most features.

## When something breaks

| Symptom | Start here |
| --- | --- |
| Workspace not found or wrong root | `internal/store/discover.go`, startup flow |
| YAML/script missing or overwritten | `internal/store/store.go`, `internal/store/save.go`, storage flow |
| Variable has the wrong value | `internal/vars/store.go`, engine folder-variable tests |
| Request differs on the wire | `internal/vars/resolve.go`, `internal/httpx/client.go` |
| `pm.*` mismatch or hang | `internal/script/bridge.go`, `internal/script/run.go`, compatibility corpus |
| UI key does nothing | focused pane keys, `internal/tui/keys.go`, generated keys test |
| UI freezes or races | effect accidentally in Update/View; inspect commands and context |
| Secret appears in output | `internal/history/redact.go`, `internal/qa/secret_leak_test.go` |
| Runner order/exit wrong | `internal/engine/run.go`, reporter event handling |
| Import loses shape | converter tests and `docs/postman-local-format.md` |

## Glossary

- **workspace** — discovered directory containing `postctl.yaml`.
- **collection / folder / request** — ordered model tree; IDs are stable,
  filenames are storage details.
- **environment** — selectable variable set, optionally marked production.
- **scope** — variable layer; higher-precedence layers shadow lower ones.
- **secret ref** — opaque Keychain lookup stored instead of plaintext.
- **hydrate** — read a secret into an in-memory copy on demand.
- **mask** — replace secret-bearing output before persistence/presentation.
- **golden** — checked-in expected output intentionally updated by a test flag.
- **ledger** — append-only implementation history, not the binding plan.
- **worktree** — separate checkout attached to a branch for isolated work.
