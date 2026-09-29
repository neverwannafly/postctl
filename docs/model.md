# postctl — domain model and on-disk format

This document is the contract for `internal/model` (pure types) and `internal/store` (YAML/JS files on disk). Every later workstream builds on it. The godoc on each exported identifier is authoritative for field semantics; this page gives the shape, the rules, and examples.

Schema version: **1** (`model.SchemaVersion`). Struct tags in `internal/model` *are* the YAML format; renaming a tag is a schema change and needs a migration (`store.Migration`).

## Type diagram

```text
Workspace                       id, name, description, defaults (RequestSettings), extra
├── Collections []*Collection   id, name, description, version, auth, variables, scripts, extra
│     └── Items []Item          ordered; *Folder | *Request
│           ├── Folder          id, name, description, auth, variables, scripts, extra, Items []Item
│           └── Request         id, name, description, kind, method, extra
│                 ├── URL       raw, query []KV (incl. disabled), pathVariables []Variable
│                 ├── Headers   []KV {key, value, description, disabled}
│                 ├── Body      mode, raw+language, urlencoded []KV, formData []FormPart,
│                 │             file, graphql{query,variables}, disabled, extra
│                 ├── Auth      *Auth (nil = inherit)
│                 ├── Scripts   {preRequest, test} *Script
│                 ├── Settings  RequestSettings (tri-state pointers)
│                 └── Examples  []Example (saved responses, originalRequest, cookies)
├── Environments []*Environment id, name, production, variables []Variable, extra
└── Globals *Globals            id, name, variables []Variable, extra

Auth ── type: inherit | none | bearer | basic | apikey | digest | oauth2 | awsv4 | hawk | ntlm | unsupported
        one typed field set (bearer, basic, apikey, digest, oauth2, awsv4, hawk, ntlm)
        originalType + raw map[string]any  → anything the typed fields do not model
OAuth2Config ── grantType: client_credentials | authorization_code | authorization_code_pkce | password | implicit
        endpoints, client, scope, redirect, PKCE, placement, token *OAuth2Token
Variable ── key, value, type (default|secret), dataType, description, disabled, secretRef
```

`Item` and `Container` are sealed interfaces (`*Folder`, `*Request`; `*Collection`, `*Folder`). An item's index in its parent's `Items` slice is its order.

## Rules every consumer must follow

1. **IDs.** Every node has a stable `model.ID` (string). Importers keep Postman IDs; new nodes use `model.NewID()` (UUIDv7, strictly increasing within a process). Hand-written files without `id` get `model.DeriveID(parent, name)`, a deterministic value that becomes permanent on the next save. IDs are unique within a collection.
2. **Zero values are safe.** `Disabled` (not `Enabled`) on `KV`, `Variable`, `FormPart` means a literal is enabled by default. A nil `*bool`/`*int`/`*Duration` in `RequestSettings` means "use the workspace default" (`Workspace.Defaults`, then built-in). A nil `*Auth` on a request or folder means **inherit**; a nil collection `Auth` means no auth. `Body{}` (mode empty) is no body. `model.KindHTTP` must be set explicitly (`Validate` rejects an empty kind; `NewRequest` sets it).
3. **`Extra` / `Auth.Raw` keep unknown data.** Importers must put fields they do not model here rather than dropping them (Postman `proxy`, `certificate`, `protocolProfileBehavior` extras, unknown auth parameters, environment `_postman_*` keys). YAML decoding is strict, so anything not modelled *and* not in an `extra` map is an error on load.
4. **URL.** `Raw` is what is sent. `Query` lists all parameters in order, **including disabled ones** (which are not in `Raw`); use `URL.EnabledQuery()` and `URL.SyncRaw()` when editing. `model.ParseURL(raw)` builds a URL with query and `:path` variables.
5. **Scripts.** `Script.Source` is the program text (Postman `exec` lines joined with `\n`; `ScriptFromExec` / `Script.Exec`). It is **never** written to YAML: the store writes a sibling `.js` file and records its name in `Script.File`, which the store sets on load and derives on save. Postman allows several events of one kind; importers concatenate them, in order, into one `Source`. `Language` empty means JavaScript. Descriptions are plain strings; Postman's description `type` (text/markdown/html) is not preserved.
6. **Secrets.** A `Variable` with `Type: model.VarSecret` holds its value in `Value` in memory only. On save the store moves it to the `SecretStore` and writes `secretRef` with an empty value. OAuth 2.0 token strings (`AccessToken`, `RefreshToken`, `IDToken`) are `yaml:"-"`; only metadata and `secretRef` reach YAML. `Store.Load` never touches Keychain; call `Store.Hydrate` (or `HydrateVariables` / `HydrateAuth`) when values are needed. When cloning a variable or auth, clear `SecretRef`/`Token.Ref` so the copy does not share the original's secret.
   Literal credentials typed into auth fields (bearer token, basic password, client secret...) are written to YAML as-is, exactly as in a Postman export. Prefer `{{secretVariable}}` references.
7. **Validation.** `Workspace.Validate`, `Collection.Validate`, `Request.Validate`, `Environment.Validate` and `Globals.Validate` return a `*model.ValidationError` listing every problem. The store validates before writing.
8. **Structural edits** use `Collection.Insert/Remove/Move/Find/Walk/Requests` on the in-memory tree, then persist with `Store.SaveCollection` (or `SaveRequest` for a content-only edit).
9. **Copies.** `Clone()` (on `Request`, `Folder`, `Collection`, `Environment`, `Globals`, and the parts `Auth`, `OAuth2Config`, `Body`, `URL`, `Scripts`, `Script`, `RequestSettings`, `Example`) returns a deep copy that shares no slice, map or pointer with the original and keeps IDs and secret refs; use it for undo snapshots and edit buffers. `CloneFresh()` (on `Request`, `Folder`, `Collection`, `Environment`) is for duplicate actions: it assigns new IDs recursively (nodes, examples, script IDs), clears `SecretRef` and `OAuth2Token.Ref` (rule 6) and `Script.File`, and keeps the in-memory secret values, so hydrate first if the copy should keep its secrets; saving the copy stores them under new refs. `CloneItem` / `CloneItemFresh` do the same for an `Item`. Clone tests are reflection-driven, so a new model field that is not cloned deeply fails them.

## Postman v2.1 mapping (for the importer)

| Postman | Model |
| --- | --- |
| `info._postman_id`, `info.name`, `info.description`, `info.version` | `Collection.ID/Name/Description/Version` |
| other `info.*`, unknown top-level keys | `Collection.Extra` |
| `item[]` with `item` children | `Folder` (nested); `id`, `name`, `description`, `auth`, `variable`, `event` |
| `item` with `request` | `Request` (`kind: http`) |
| `request.method` | `Request.Method` |
| `request.url` (string or object) | `URL.Raw`; `url.query[]` → `URL.Query` (`disabled` kept); `url.variable[]` → `URL.PathVars`; host/path/port parts are derivable and not stored |
| `request.header[]` | `Headers []KV` (`disabled`, `description`) |
| `body.mode` = raw / urlencoded / formdata / file / graphql / none | `BodyRaw` / `BodyURLEncoded` / `BodyFormData` / `BodyBinary` (`Body.File` = `file.src`) / `BodyGraphQL` / `BodyNone` |
| `body.options.raw.language` | `Body.Language` |
| `formdata[]` `{type: text|file, value, src, contentType}` | `FormPart` |
| `auth.type` noauth / bearer / basic / apikey / digest / oauth2 / awsv4 / hawk / ntlm | `AuthNone` / `AuthBearer` / `AuthBasic` / `AuthAPIKey` / `AuthDigest` / `AuthOAuth2` / `AuthAWSV4` / `AuthHawk` / `AuthNTLM` |
| any other auth type (oauth1, edgegrid, jwt, asap, akamai...) | `AuthUnsupported` + `OriginalType` + `Raw` |
| no `auth` on a request/folder | `Auth == nil` (inherit) |
| `oauth2.grant_type` authorization_code / authorization_code_with_pkce / client_credentials / password_credentials / implicit | `GrantAuthorizationCode` / `GrantAuthorizationCodePKCE` / `GrantClientCredentials` / `GrantPassword` / `GrantImplicit`; an imported `accessToken` goes into `OAuth2Config.Token.AccessToken` (the store moves it to Keychain) |
| `event[]` `listen: prerequest|test` `script.exec` | `Scripts.PreRequest` / `Scripts.Test` via `ScriptFromExec`; `script.id/name/src`, `event.disabled` map to `Script.ID/Name/Src/Disabled` |
| `variable[]` (`key`, `value`, `type`, `disabled`, `description`) | `Variable` (`type` string/number/boolean/any → `DataType`) |
| `protocolProfileBehavior` | `RequestSettings` (known keys) and `RequestSettings.Extra` |
| `request.proxy`, `request.certificate` | `Request.Extra` |
| `response[]` (examples) | `Example` (`originalRequest`, `status`, `code`, `header`, `cookie`, `body`, `_postman_previewlanguage`) |
| environment `values[]` `{key, value, type: default|secret|any, enabled}` | `Environment.Variables` (`enabled: false` → `Disabled`; `any` → default) |
| environment / globals `_postman_*` keys | `Environment.Extra` / `Globals.Extra` |

## On-disk layout

```text
<workspace>/
  postctl.yaml                      id, name, defaults; collections: [display order of dir names]
  globals.yaml
  environments/<slug>.yaml          sorted by name on load
  collections/<slug>/
    collection.yaml                 metadata; order: [child entry names]
    pre.js  test.js                 collection-level scripts
    <NNN>-<slug>.yaml               one request per file
    <NNN>-<slug>.pre.js             request scripts share the request's file stem
    <NNN>-<slug>.test.js
    <folder-slug>/folder.yaml       nested folder (same shape, recursively); pre.js / test.js
  .postctl/                         local state (history, cookies, state.yaml); gitignored
```

- **Ordering.** A container's `order:` list (child entry names: request stems and folder directory names) wins. Entries not listed follow, sorted by numeric filename prefix, then name. The store always writes both: `NNN` is the 1-based position among *all* siblings (zero-padded to at least 3 digits), so the fallback and the explicit list agree.
- **Names.** File and folder names are derived from node names with `store.Slug` (lower-case ASCII, `-` separators, max 60 bytes). Collisions get `-2`, `-3`, ... An existing entry keeps its name while it is still the slug or a collision variant of it, so unrelated saves never rename files. The store owns these names: renaming a node or reordering siblings moves files on the next save and removes files it no longer owns. Files it does not recognise (README.md, ...) are never touched.
- **Script files.** Written as `Source + "\n"`; one trailing newline is stripped on read, so text round-trips exactly. A script entry with no `file:` adopts the conventional sibling file if present, so a hand-dropped `pre.js` works without YAML edits. `file:` may not escape its directory.
- **Discovery.** `store.Discover(cwd, opts...)` picks the first match of: `store.WithWorkspace(dir)` (the global `--workspace DIR` flag; an empty value is ignored), `$POSTCTL_WORKSPACE`, a `postctl.yaml` found walking up from `cwd`, then the global dir: `$POSTCTL_HOME`, else `$XDG_CONFIG_HOME/postctl` (absolute paths only), else `~/.config/postctl`. An explicit workspace (flag or env) skips discovery entirely and need not be initialized yet: check `Location.Initialized` and call `store.Init`. `Location.Source` is `SourceFlag`, `SourceEnv`, `SourceProject` or `SourceGlobal`. `$POSTCTL_HOME` only moves the global fallback. `store.Init` creates a workspace and a `.gitignore` containing `.postctl/`.
- **Strict and lenient loading.** `Load` fails on the first bad file with a `*FileError` (the CLI uses it). `LoadLenient` returns `(ws, []*FileError, error)` for the TUI: a bad request file, folder (`folder.yaml`; its subtree goes with it), collection (`collection.yaml`), environment or globals file, or a request whose `.js` file is missing, is skipped and reported, and everything else loads. Each `FileError` has the absolute `Path` and an `Err` that works with `errors.Is` (for example `ErrNewerSchema`). The error result is non-nil only when the workspace itself is unloadable (`postctl.yaml` missing or malformed); `ws` is then nil. Saving a leniently loaded workspace is safe: `Save`, `SaveCollection` and `SaveRequest` never delete a file that `LoadLenient` reported (a broken request, folder, collection or environment, the children of a broken folder, or a request whose script file is missing), so the TUI can autosave edits to the nodes that did load and the user can still fix the rest. Only the explicit `DeleteCollection` / `DeleteEnvironment` remove broken files.
- **Atomicity and determinism.** Each file is written via temp file + rename in the same directory; identical content is not rewritten. Output is a pure function of the model (struct field order, sorted map keys, two-space indent, block scalars for multi-line text), so saving twice, or save → load → save, is byte-identical.
- **Schema versions.** `schemaVersion` is in `postctl.yaml`, `collection.yaml`, environments and globals; folder and request files use their collection's. A newer version fails with `store.ErrNewerSchema`; older versions run registered `store.Migration`s (none yet).

## Examples

`postctl.yaml`

```yaml
schemaVersion: 1
id: ws-1
name: payments-api
defaults:
  followRedirects: true
  timeout: 30s
collections:
  - payments-api
  - payments-api-2
```

`collections/payments-api/collection.yaml`

```yaml
schemaVersion: 1
id: coll-pay
name: Payments API
version: 1.2.0
auth:
  type: bearer
  bearer:
    token: '{{token}}'
variables:
  - key: baseUrl
    value: https://api.test
  - key: apiSecret
    type: secret
    secretRef: collection/coll-pay/apiSecret   # value lives in Keychain
scripts:
  preRequest:
    file: pre.js
order:
  - charges
  - 002-ping
```

`collections/payments-api/charges/001-list-charges.yaml`

```yaml
id: req-list
name: List charges
kind: http
method: GET
url:
  raw: '{{baseUrl}}/v1/charges/:id?limit=10&expand=customer'
  query:
    - key: limit
      value: "10"
    - key: debug          # disabled: present here, absent from raw
      value: "1"
      disabled: true
  pathVariables:
    - key: id
      value: ch_1
headers:
  - key: X-Trace
    value: abc
    disabled: true
auth:
  type: inherit
scripts:
  preRequest:
    file: 001-list-charges.pre.js
  test:
    file: 001-list-charges.test.js
settings:
  timeout: 5s
  followRedirects: false
```

A raw JSON body is a block scalar:

```yaml
body:
  mode: raw
  language: json
  raw: |-
    {
      "amount": {{amount}},
      "currency": "usd"
    }
```

OAuth 2.0 with a stored token (the token strings are in Keychain under `oauth2/req-oauth`):

```yaml
auth:
  type: oauth2
  oauth2:
    grantType: authorization_code_pkce
    authUrl: https://idp.test/authorize
    accessTokenUrl: https://idp.test/token
    clientId: cid
    clientSecret: '{{csecret}}'
    scope: read write
    token:
      tokenType: Bearer
      expiresAt: 2026-09-30T12:00:00Z
      secretRef: oauth2/req-oauth
```

`environments/prod.yaml`

```yaml
schemaVersion: 1
id: env-prod
name: Prod
production: true          # UI shows a warning badge; never blocks sending
variables:
  - key: baseUrl
    value: https://api.example.com
  - key: apiKey
    type: secret
    secretRef: env/env-prod/apiKey
```

More complete samples (every body mode, auth type, disabled rows, scripts, examples) are the golden files in `internal/store/testdata/golden/workspace/`; regenerate them with `go test ./internal/store -run TestGolden -update`.

## Store API in one screen

```go
loc, _ := store.Discover(cwd, store.WithWorkspace(flagDir)) // Location{Root, Source, Initialized}; flag > $POSTCTL_WORKSPACE > walk up > global
s, _ := store.Open(loc.Root, store.WithSecretStore(store.NewKeychain("")))
s, _ = store.Init(dir, "name", opts...)                // new workspace

ws, _ := s.Load()                                      // strict (CLI); *store.FileError names the bad file
ws, problems, _ := s.LoadLenient()                     // TUI: skips and reports bad files ([]*store.FileError)
missing, _ := s.Hydrate(ctx, ws)                       // fill secret values from Keychain
_ = s.Save(ctx, ws)                                    // full sync (removes what is no longer in ws)
_ = s.SaveCollection(ctx, c)                           // create / rename / move / delete items
_ = s.SaveRequest(ctx, c, id)                          // fast path for content-only edits
p, _ := s.PathOf(c, id)                                // YAML path, for $EDITOR
_ = s.SaveEnvironment(ctx, env); _ = s.SaveGlobals(ctx, g)
_ = s.DeleteCollection(ctx, id); _ = s.DeleteEnvironment(ctx, id)
st, _ := s.LoadState(); _ = s.SaveState(st)            // .postctl/state.yaml (active environment)
```
