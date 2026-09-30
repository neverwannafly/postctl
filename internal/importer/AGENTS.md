# internal/importer — local rules

Read the root [`AGENTS.md`](../../AGENTS.md), [`docs/flows/import.md`](../../docs/flows/import.md), [`docs/model.md`](../../docs/model.md) (the Postman mapping table) and [`docs/postman-local-format.md`](../../docs/postman-local-format.md) first.

Two packages, deliberately separate:

| Package | Job |
| --- | --- |
| `postmanlocal/` | reverse-engineered, fragile: copies the Postman desktop IndexedDB to a private temp dir, decodes LevelDB/Blink/V8 values, emits **Collection v2.1 / environment / globals documents** plus a coverage report (`extract.go`, `indexeddb.go`, `v8.go`, `locate.go`, `staging.go`) |
| `postman/` | stable: converts v2.0/v2.1 documents into `internal/model` (`converter.go`), moves literal credentials into secret variables (`secrets.go`), marks credential-looking variable names secret (`secretnames.go`, plan D11), warns about empty inputs (`emptyinputs.go`), merges by stable ID (`import.go`), reports counts and names (`report.go`) |

`cmd/postctl/import.go` (`runImport`) wires them: file import reads JSON and calls `postman.Convert`; `postman-local` calls `postmanlocal.Extract` and feeds each document to `postman.ConvertObject`; the merge, dry run and saving happen there too.

## The JSON-shaped rule (read this twice)

`import postman-local` once silently lost **all script code**. The extractor built each event's `exec` as a Go `[]string` (`strings.Split`), but the converter only accepted the shape `encoding/json` produces (`[]any`) and treated any other type as empty. Every test fed the converter JSON files, which decode to `[]any`; none ran the extractor's in-memory output through the converter, so the suite stayed green while the real command dropped scripts.

It is fixed twice, on purpose (defense in depth): the producer hands out only JSON shapes (`shapeDocument` in `postmanlocal/jsonshape.go`, called from `buildResult`; `execLines` returns `[]any`), and the consumer normalizes (`ConvertObject` round-trips its input through `encoding/json`, and errors on a value JSON cannot carry instead of dropping it). Keep both.

Rules:

1. Any value that crosses the `postmanlocal` to `postman` boundary, or any decoder boundary in this tree, is **JSON-shaped**: `map[string]any`, `[]any`, `string`, `float64` or `json.Number`, `bool`, `nil`. Never put `[]string`, `[]map[string]any`, typed ints or structs into a document map. If a producer cannot guarantee it, **normalize at the boundary**.
2. Converters must not treat an unexpected dynamic type as "empty" silently. Count it and surface it in the `Report` (as a warning with a path and a code, never a value), or fail.
3. Real-data and acceptance tests must run **the same code path users run**: `postmanlocal.Extract` output (in memory, nothing staged as JSON) through `ConvertObject`, merge and `store.Save`, asserting **non-zero counts** for requests, scripts, variables and environments. A staged JSON copy only tests the file path. Test both paths for every field you add.
4. When a bug hides in a type mismatch, add the regression test on the production path first, then fix.

Regression tests to keep green and to copy from: `TestConvertObjectKeepsScriptsFromGoTypedDocument`, `TestConvertObjectMatchesJSONRoundTrip` (`postman/gotypes_test.go`), `TestBuildResultEmitsOnlyJSONShapes`, `TestJSONShapeConversions` (`postmanlocal/extract_test.go`), and the private end-to-end `TestPrivateLocalExtractInMemoryToStore` and `TestPrivateImportPostmanLocalCommand`.

## Other invariants

- **Never print values.** Reports, errors and test failures carry counts, names, paths and codes, never URLs with queries, tokens, bodies or header values. Real Postman data is company data: read-only, temp copy only, counts only (plan R4). Committed fixtures are synthetic (`testdata/*.json`).
- **D2 secrets** (plan §0): literal credentials (bearer, basic, digest, NTLM, API key, OAuth2 secrets and verifiers, AWS secret key and session token, Hawk key, credential-named params of unsupported schemes) become generated secret collection variables and the field becomes `{{name}}`; `--keep-literal-secrets` opts out. Counts come from what was actually moved (`TestImporterMovesEveryAuthSecret`). New auth fields with secrets must be added to `moveAuthSecrets`.
- **Re-import overwrites by stable ID** (plan Q3): a document replaces the node with the same ID; items missing from the export are removed; the dry run reports exactly what a real import does; `.postctl/` is untouched; a locally fetched OAuth token stays attached. Removed nodes' secrets are cleaned up from the Keychain.
- **Name-based secrets (plan D11)**: variables whose key looks like a credential (`LooksSecretName`) become `secret`; `--no-mark-secrets` opts out, `--keep-literal-secrets` keeps valued variables plain. Reports list key names, never values (`TestReportListsMarkedNamesNeverValues`, `TestMarkedSecretsNeverReachYAML`).
- **Keep unknown data**: anything not modelled goes into `Extra` or `Auth.Raw`, not the floor (model rule 3).
- This package never touches the Keychain: only `store` does, on save.

## Tests

```sh
go test -race ./internal/importer/... ./cmd/postctl
UPDATE_GOLDEN=1 go test ./internal/importer/postman   # golden workspace in testdata/golden; review the diff
go test ./internal/importer/... -run Private -v       # real data; skips when testdata/private is absent
```

- Golden workspace: `TestGoldenWorkspace` (note the env var, not `-update`).
- Private data tests: `TestPrivateStaging` and `TestPrivateIndexedDB` need the gitignored `testdata/private`; `TestPrivateLocalExtractInMemoryToStore` and `TestPrivateImportPostmanLocalCommand` read the local Postman install read-only (copied to a temp dir first). All skip when the data is absent and print counts only. Use `t.Fatal("fixed message")`, never `t.Fatalf` with document content.
- The extractor tests use synthetic LevelDB and V8 bytes (`extract_test.go`, `v8_test.go`).
