# WS1 — Postman importer (v2.1 JSON → postctl workspace) + `postctl import`

Owner model: GPT-5.6 Sol (medium) · Branch: `ws/1-importer` · Worktree: `~/work/postctl-wt/ws1-importer`

## Goal

Turn Postman Collection v2.0/v2.1, environment and globals JSON into `internal/model` values, persist them with `internal/store`, and print an honest import report. Wire the `postctl import` subcommand for both sources:

- `postctl import postman <files or dirs…>` (exported JSON)
- `postctl import postman-local` (uses the merged `internal/importer/postmanlocal` package to extract from the Postman desktop cache, then imports the result)

## Read first

- `docs/plan.md` §0 (binding; especially Q3, D2, R4), §6 (import), §7 (scripts).
- `docs/model.md`, especially "Rules every consumer must follow" and the "Postman v2.1 mapping" table. That table is your spec; if you find a gap, extend the table in the same change.
- `internal/model`, `internal/store` code (godoc is thorough).
- `internal/importer/postmanlocal` public API (`Locate`, `Extract`, `Result`, `Coverage`) and `docs/postman-local-format.md`.
- `docs/design-language.md` → "Import report" sketch (the CLI report should read like it, linear).

## Scope (files you own)

- `internal/importer/postman/` — parser + converter + report.
- `cmd/postctl` — **only** the `import` subcommand wiring (keep other stubs intact).
- `internal/importer/postman/testdata/` — synthetic fixtures.
- `docs/model.md` — mapping-table corrections only.

## Requirements

1. Parse v2.1 and v2.0 collections (detect via `info.schema`), environments (`_postman_variable_scope: environment`), globals. Reject unknown formats with a clear error.
2. Map everything in the mapping table losslessly: nested folders and order, requests, URL (raw + structured, disabled query params, path variables), headers (disabled), all body modes (raw + language, urlencoded, form-data text/file, binary, GraphQL), auth (all types, `inherit` vs `noauth` semantics, unknown params into `Auth.Raw`), variables at collection level, events (`prerequest`/`test`, multiple events of one kind concatenated in order, `disabled` events respected), saved examples, descriptions, `protocolProfileBehavior` into `RequestSettings` + `Extra`, anything else into `Extra`.
3. **Secrets (D2):** move literal secret values from auth fields into generated secret collection variables (`VarSecret`, saved to Keychain through the store's `SecretStore`) and rewrite the field to `{{name}}`. Name them predictably (`<authType>_<field>`, deduplicated). Environment values with Postman `type: secret` → `VarSecret`. `--keep-literal-secrets` disables this. Never print secret values in the report.
4. **Script lint:** statically scan script sources for APIs outside plan §7 tiers T1+T2 (e.g. `pm.visualizer`, `pm.vault`, `pm.require`, `require('<unknown>')`, legacy `tests[`, `responseBody`, `postman.getEnvironmentVariable`). Report per request as warnings (the script is still imported).
5. **Re-import:** match by Postman ID; overwrite existing items; `--dry-run` prints what would be added, changed or removed, and writes nothing. Items only in postctl (not in the source) are kept, and reported as "kept (not in source)".
6. **Report:** summary counts (collections, folders, requests, scripts pre/test, examples, environments, secrets moved to Keychain) plus a warnings list (unsupported auth, linted script APIs, unresolved items from the local cache coverage). `--json` gives a machine-readable version. Exit code 0 on success with warnings, 2 on partial failure, 3 on usage errors.
7. `postman-local`: call `postmanlocal.Locate` + `Extract`, feed its in-memory v2.1 values (or staged JSON) into the same converter, merge its `Coverage` gaps into the report. Flags: `--partition`, `--dry-run`.
8. Workspace target: `--workspace DIR` (plan D1), otherwise discovery or the global default. Create the workspace with `store.Init` if it's missing.

## Tests

- Table-driven unit tests per mapping row using synthetic fixtures (write them by hand; include nasty cases: empty `item`, folder-level auth/scripts, `{{var}}` in host, duplicated header keys, disabled events, a v2.0 file).
- Golden test: import fixture → save → compare the YAML tree to golden files.
- Real-data test: if `testdata/private/staging/` exists (copy it from `/Users/shubham/work/postctl/testdata/private/staging` into your worktree; it's gitignored), import it into a temp workspace and assert the counts match the coverage in the ledger (2 collections, 23 requests, 17 with pre-request scripts, 3 environments) and that loading the workspace back succeeds. **Never print values from it.** Skip when absent.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green; `postctl import postman-local --dry-run` works on this machine and prints only names and counts; ledger entries (STARTED, PROGRESS, DONE) in your worktree's `docs/ledger.md`.

## Parallel agents (don't touch their files)

WS0.1 (store/model: `LoadLenient`, `Clone`), WS2 (`internal/vars`, `internal/httpx`), WS2b (`internal/oauth2`), WS3 (`internal/script`), WS5 (`internal/ui`, `internal/tui`). If you need something from model/store that doesn't exist, work around it locally and list it under "Blockers / questions" rather than editing those packages.

## Final message

What shipped, CLI usage, report sample (names/counts only), deviations, open questions, test/lint results, commit hash.
