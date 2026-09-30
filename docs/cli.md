# CLI

`postctl` with no subcommand opens the terminal UI. The commands below are linear: they write to stdout and stderr, never take over the screen, and are safe to pipe. Color is off when stdout is not a terminal, when `NO_COLOR` is set, or when `--accessible` is passed. Status is always a glyph plus a word (`✓` passed, `×` failed, `!` warning). `--ascii` or `POSTCTL_ASCII=1` swaps those for `+`, `x` and `!`, and swaps the secret mask `••••` for `****`.

Paths are `<collection>/<folder>/.../<request>`. Each segment is a display name, a slug, or an ID, matched case-insensitively. A single name that is not a collection is searched through the whole tree. If more than one item matches, the command exits 3 and lists the candidates.

`-e` / `--env` selects an environment by name, slug or ID. When it is omitted, `send`, `run` and `curl` use the active environment (`postctl env use`).

Every subcommand accepts `--workspace DIR` or `-w DIR` after the command name,
for example `postctl ls --workspace ./api`. An explicit flag wins over
`POSTCTL_WORKSPACE`; without either, postctl discovers the workspace from the
current directory.

## ls

```sh
postctl ls
postctl ls Payments/Billing
postctl ls --flat          # one path per line, for fzf
postctl ls --json
```

Text output is an indented tree. Requests are `METHOD` (padded) then the name. `--flat` prints `METHOD` and the full path for requests, and the path alone for folders and collections. `--json` prints one document: `items` of `{type, id, name, path, method, children}`.

## send

```sh
postctl send Payments/Billing/Create -e staging --var amount=10
postctl send Payments/List\ charges --include --verbose
postctl send Payments/List\ charges --json --no-scripts --no-history
```

`send` accepts HTTP requests only. Open WebSocket requests in the TUI.

Stdout is the status line, then the body, then tests:

```text
200 OK · 12 ms · 11 B
{"ok":true}

✓ status is 200
```

The body is pretty-printed JSON only when stdout is a terminal. Otherwise it is raw. `--include` prints response headers before the body. `--verbose` prints the sent request (secrets masked), the timeline and console lines. `--var k=v` sets a local variable for this send. `--no-scripts` clears collection, folder and request scripts for this send. `--no-history` does not append to `.postctl/history`.

## run

```sh
postctl run Payments
postctl run Payments/Billing -e staging -n 3 --delay 200ms --bail
postctl run Payments --var region=local --var version=v2
postctl run Payments -d data.csv --reporter cli,junit --reporter-junit-export junit.xml
postctl run Payments --json
postctl run Payments --timeout-request 30s --no-history
```

`run` takes a collection or a folder. A request path is an error; use `send`.

The `cli` reporter streams each iteration and request, then a summary (iterations, requests, test scripts, prerequest scripts, assertions: executed and failed; total run duration, data received, average response time) and a numbered failures list. Ctrl-C cancels the run and still prints that summary for whatever finished.

WebSocket requests are listed as skipped and are not counted as executed
requests or failures.

`--reporter` is a comma-separated list of `cli`, `json` and `junit`. The default is `cli`. `--json` selects the json reporter when `--reporter` is omitted. `json` is NDJSON, one event per line (`run-started`, `iteration-started`, `request-started`, `request-progress`, `request-finished`, `iteration-finished`, `run-finished`). It does not include response bodies. `junit` is XML: one `testsuite` per request (name `path` or `path [#n]` when the iteration is not the first) and one `testcase` per assertion. Script and transport errors are extra failing cases.

Only one reporter may write to stdout. Pass `--reporter-json-export` or `--reporter-junit-export` when combining reporters. Repeatable `--var key=value` values seed `pm.variables` at the start of every iteration, matching `send --var`. `-d` is a CSV or JSON data file. `-n` is the iteration count (`0` uses the engine default). `--delay` is the pause between requests. `--bail` stops after the first failure. `--timeout-request` sets the request timeout on a clone of the collection.

## env

```sh
postctl env ls
postctl env use staging
postctl env show staging
postctl env show staging --reveal
postctl env get staging          # same as show
postctl env set staging region us --secret
postctl env set staging token --secret      # value not on argv: prompts with no echo
postctl env unset staging region
```

`ls` marks the active environment with `*` and a production environment with `! prod`. `use` writes `store.State.ActiveEnvironment`. `get` is an alias for `show`; both mask secret values. `--reveal` prints them; when stdin is a terminal it asks you to type `y` first. `set --secret` stores the value in the secret store and does not echo it. A value on the command line ends up in your shell history, so with `--secret` you can leave it off: when stdin is a terminal, `set` prompts (`Value for token in staging (input hidden):`) and reads it with echo off; otherwise it reads one line from stdin (`pass show x | postctl env set staging token --secret`). An empty value is refused and nothing is saved. Without `--secret` the value is required. `--json` works on `ls`, `show`, and `get`.

## import

```sh
postctl import postman ~/Downloads/*.json --env Staging.postman_environment.json --workspace ./api --dry-run
postctl import postman-local --dry-run
postctl import postman-local
postctl import postman-local --no-mark-secrets
```

`import postman <files or dirs...>` reads exported collections (v2.0/v2.1), environments (`--env FILE`, repeatable) and globals (`--globals FILE`). `import postman-local` extracts them from the local Postman desktop cache (read-only, from a temporary copy). Both overwrite by stable Postman ID, and `--dry-run` reports exactly what a real import would add, change and remove without writing. `--json` prints the report as one JSON document. Flags: `--workspace DIR` / `-w DIR`, `--partition ID` (`postman-local` only), `--dry-run`, `--json`, `--keep-literal-secrets`, `--no-mark-secrets`.

Secrets:

- Literal credentials in auth fields move to generated secret collection variables in the Keychain (`Secrets N moved to Keychain variables`). `--keep-literal-secrets` opts out, and also keeps a variable that has a value plain when its name looks secret.
- Variables whose key looks like a credential are marked `secret` (`Secrets · N marked secret by name`, with the key names per environment, collection, folder or globals). The rule works on the words of the key, case-insensitively and across `snake_case` / `camelCase`: a word that is or ends with `token`, `secret`, `password`, `passwd`, `passphrase`, `cookie` or `credential`; the words `session`, `jwt`, `bearer`, `authorization`, `apikey`, `pwd`; or the pairs `api key`, `private key`, `secret key`, `signing key`, `encryption key`, `master key`, `auth key`. A key is not marked when a later word describes rather than holds the credential (`url`, `uri`, `endpoint`, `name`, `type`, `scope`, `prefix`, `expires`/`expiry`, `ttl`, `timeout`, `path`, ...), so `token_url`, `auth_url`, `session_timeout` and `cookie_name` stay plain. An empty variable only gets the type; a value it has goes to the Keychain. `--no-mark-secrets` turns this off. Re-importing is idempotent and keeps values you stored.
- Values are never printed, only key names.

After the summary, warnings list empty inputs, by key name only, per environment: a variable that requests or scripts read, that no imported environment, globals or collection gives a value, and that no script sets, for example ``auth_token is empty in Preview; set it with `postctl env set Preview auth_token --secret` ``.

## curl

```sh
postctl curl Payments/List\ charges -e staging
postctl curl Payments/List\ charges --with-secrets
```

Prints one shell-quoted curl command for the resolved request. Secret variable values and credential fields are masked unless `--with-secrets` is set. Hawk auth is not expanded into curl flags.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | The command finished and every test passed. |
| 1 | A test or a script failed. |
| 2 | A request or the transport failed, or the run was canceled before any failure. |
| 3 | Usage or configuration: unknown command, ambiguous path, missing environment, bad flag. |

A canceled run that already saw test failures still exits 1. A canceled run that already saw a transport error still exits 2.

## Differences from newman

- `--json` is a stream of NDJSON events, not one JSON document at the end.
- There is no HTML reporter. Reporters are `cli`, `json` and `junit`.
- Folder and request selection is the path argument. There is no `--folder`.
- Test-script and prerequest-script counts are the script steps that ran in this process. Assertion, request and iteration failures come from the run summary.
- A run canceled with no failures exits 2 so a CI job is not green.
- Two reporters cannot share stdout; the second needs `--reporter-json-export` or `--reporter-junit-export`.
- `env` has `show` (alias `get`), `unset` and `--reveal`. Secrets stay masked unless you pass `--reveal` or `curl --with-secrets`.
- Data rows are reused when `-n` is larger than the file (engine behavior).
