# Migrating from Postman to postctl

This guide walks through transitioning from Postman and Newman to `postctl`. It maps Postman concepts to `postctl`, explains how automated data and secrets migration work, documents script compatibility, and compares CLI workflows.

---

## 1. Concept Mapping

`postctl` translates Postman's cloud and desktop abstractions into a filesystem-first, git-friendly model.

| Postman Concept | `postctl` Equivalent | How It Works |
| --- | --- | --- |
| **Workspace** | Workspace directory (`postctl.yaml`) | A local directory tree containing `postctl.yaml`. Can be checked into git right alongside your service code. Discoverable by walking up from `cwd` or set via `--workspace` / `POSTCTL_WORKSPACE`. |
| **Collection** | `collections/<slug>/collection.yaml` | Each collection is a directory with a `collection.yaml` file that specifies child ordering, variables, and collection-level auth and scripts (`pre.js`, `test.js`). |
| **Folder** | Subdirectory with `folder.yaml` | Nested directories inside a collection, each with `folder.yaml`, ordering child requests or subfolders, with optional folder-level auth and scripts. |
| **Request** | `<NNN>-<slug>.yaml` + `.js` scripts | One YAML file per request (e.g. `001-list-charges.yaml`). Pre-request and test scripts live in sibling JavaScript files (`001-list-charges.pre.js`, `001-list-charges.test.js`). |
| **Environments** | `environments/<slug>.yaml` | Individual YAML files containing environment variables. Active environment is tracked locally in `.postctl/state.yaml` and displayed in the TUI header (`● staging`, `! prod`). |
| **Globals** | `globals.yaml` | Workspace-wide variables in `globals.yaml`. Available to all collections unless overridden by an environment or collection variable. |
| **Collection Variables** | `collection.yaml` (`variables:`) | Scoped to requests within that collection. Stored directly in `collection.yaml`. |
| **Script Execution Order** | Pipeline: Collection → Folders → Request | Identical to Postman: collection pre-request script runs first, then outer-to-inner folder scripts, then request pre-request script. Test scripts execute in the same order upon response arrival. |
| **Collection Runner** | TUI Runner (`R`) or `postctl run` | Multi-request execution with iteration counts, data files, delays, bail on failure, and live progress reporting. |
| **Newman CLI** | `postctl run <path>` | Native CLI runner sharing the same Go engine as the TUI, with streaming CLI, NDJSON, and JUnit XML reporters. |

---

## 2. Importing Your Data

`postctl` offers two import paths: reading from your existing local Postman Desktop installation, or importing exported JSON files.

### Option A: Extract from Local Postman Desktop

If you use the Postman Desktop app on macOS, `postctl` can extract your cached collections, environments, and globals directly without manual export:

```sh
# Dry run to inspect what will be imported
postctl import postman-local --dry-run

# Run full import into current workspace
postctl import postman-local
```

**How it works & caveats:**
- **Scripts included**: collection, folder and request pre-request and test scripts are imported as `.js` files, exactly as with an exported file.
- **Read-only and safe**: Postman's active Chromium IndexedDB is copied to a temporary, mode-0700 directory before reading. Your Postman Desktop installation is never modified or locked.
- **Cache-dependent**: Desktop IndexedDB contains data cached from Postman cloud sync. If a collection was not opened locally or its body was evicted, `postctl` reports missing items in the import summary.
- **Fallback**: If collections are missing or partial, use Postman's **File → Export** to produce a Collection v2.1 JSON file.

### Option B: Import Exported JSON Files

Import collections, environments, and globals exported from Postman:

```sh
# Import one or more collections
postctl import postman ~/Downloads/MyCollection.postman_collection.json

# Import collection with specific environment and globals
postctl import postman ~/Downloads/MyCollection.json \
  --env ~/Downloads/Staging.postman_environment.json \
  --globals ~/Downloads/workspace.postman_globals.json \
  --workspace ./my-service
```

- Supports Postman Collection format **v2.0** and **v2.1**.
- Safe re-imports: Nodes are identified by stable ID. Unmodified files are not touched, preserving atomic git diffs.
- Pass `--dry-run` to preview additions, modifications, and removed files before writing.

---

## 3. Secrets Migration (Decision D2)

In standard Postman exports, sensitive credentials (passwords, bearer tokens, API keys) are frequently serialized as plaintext strings inside JSON files.

`postctl` enforces a secure-by-default secrets policy (**Plan §0, D2**):

1. **Automatic Credential Extraction**:
   During import, literal credential values in authentication blocks are detected:
   - Bearer token
   - Basic authentication password
   - API key values
   - Digest password
   - OAuth 2.0 client secret, user password, access tokens, refresh tokens
   - AWS v4 secret access key
   - Hawk authentication key
   - NTLM password

2. **Keychain Storage**:
   The detected secret value is moved to the macOS Keychain (using `/usr/bin/security`).
   On disk, the collection variable is saved with `type: secret` and an opaque `secretRef`. The YAML file contains no sensitive tokens.

3. **Template Rewriting**:
   The authentication field in the request or folder is automatically rewritten to reference the generated secret variable:
   ```yaml
   # Before (Postman raw export):
   auth:
     type: bearer
     bearer:
       token: "super-secret-token-12345"

   # After (postctl imported YAML):
   auth:
     type: bearer
     bearer:
       token: "{{bearer_token}}"
   ```

4. **Environment Secrets**:
   Variables defined with `type: secret` in Postman environments are imported as `model.VarSecret`. Their values are stored directly in the Keychain, leaving the `environments/<env>.yaml` file safe to commit to version control.

5. **Secret-Looking Variable Names**:
   Postman often types tokens as `default`, and it does not keep current values locally, so such variables import empty. So that a script's `pm.environment.set("krakenD_token", ...)` is never written to a plaintext YAML file, the importer marks environment, globals, collection and folder variables as `secret` when their key looks like a credential: it contains a word such as `token`, `secret`, `password`, `apikey` / `api_key`, `cookie`, `session`, `jwt`, `bearer`, `authorization`, `credential` or `private_key` (`authToken`, `CF_COOKIE_HEADER` and `client_secret` match; `token_url`, `auth_url`, `session_timeout` and `cookie_name` do not). The exact rule is documented in `internal/importer/postman/secretnames.go`. An empty variable just gets the type; a value it already has goes to the Keychain like any other secret. The report lists the key names it marked (never values):
   ```text
   Secrets · 3 marked secret by name
     Environment › Preview: auth_token, krakenD_token
   ```
   Re-importing is idempotent: it reports 0 changes, and a value you stored stays in the Keychain.

6. **Opt-Outs**:
   If you must preserve raw, plaintext credentials in your YAML files (for example, in an isolated offline testing fixture), pass:
   ```sh
   postctl import postman <file.json> --keep-literal-secrets
   ```
   With it, a variable that already has a value stays plain even when its name looks secret; empty ones still get the type. To turn off name-based marking entirely (Postman's own `secret` type still applies), pass `--no-mark-secrets` to `import postman` or `import postman-local`.

### Filling in empty variables

Because current values are not stored locally, imported environments often have empty variables that your requests need. After an import, `postctl` warns about the ones that requests or scripts read, that no imported environment gives a value, and that no script sets:

```text
Warnings
  ! auth_token is empty in Preview; set it with `postctl env set Preview auth_token --secret`
```

Set a secret without putting it in your shell history by leaving the value off the command line; `postctl` prompts with no echo, or reads one line from stdin:

```sh
postctl env set Preview auth_token --secret            # prompts; typing is hidden
pass show api/preview-token | postctl env set Preview auth_token --secret
```

Variables a script sets (such as a token fetched by a login request) are not warned about; the script fills them in and, for a secret variable, the value goes to the Keychain.

---

## 4. Scripting Compatibility

`postctl` executes JavaScript pre-request and test scripts using [goja](https://github.com/dop251/goja), an ECMAScript 5.1+ runtime written in pure Go. Scripts run in-process without requiring Node.js, npm, or external processes.

### Supported `pm.*` API

| Area | Status | Supported Methods & Behaviors |
| --- | --- | --- |
| `pm.environment` | **Supported** | `get`, `set`, `unset`, `has`, `clear`, `toObject`, `replaceIn` |
| `pm.collectionVariables` | **Supported** | `get`, `set`, `unset`, `has`, `clear`, `toObject`, `replaceIn` |
| `pm.globals` | **Supported** | `get`, `set`, `unset`, `has`, `clear`, `toObject`, `replaceIn` |
| `pm.variables` | **Supported** | Scoped lookup across local → data → env → collection → globals. `set` writes to local run scope. `replaceIn` expands `{{vars}}` and `{{$dynamic}}` tokens. |
| `pm.request` | **Supported** | `url` (segments, query parameters, path variables), `method`, `headers`, `body` (raw, urlencoded, formdata), `auth`. Mutations in pre-request scripts modify the sent request. |
| `pm.response` | **Supported** | `code`, `status`, `headers`, `json()`, `text()`, `responseTime`, `responseSize`, `to.have.status(...)`, `to.have.header(...)`. |
| `pm.test` & Chai | **Supported** | `pm.test(name, fn)`, `pm.expect(val)...`, BDD assertion styles (`to.be.true`, `to.eql`, `to.have.property`, etc.). |
| `pm.cookies` | **Supported** | `pm.cookies.has`, `pm.cookies.get`, `pm.cookies.toObject`, `pm.cookies.jar()`. Reads from persistent workspace cookie jar. |
| `pm.execution` | **Supported** | `pm.execution.skipRequest()` skips sending current request without failure. `pm.execution.setNextRequest(name)` or `pm.execution.setNextRequest(null)` controls runner flow. |
| `pm.sendRequest` | **Supported** | Send asynchronous HTTP requests from pre-request or test scripts. |

### Bundled npm Libraries

The following libraries are bundled directly into the binary and can be imported with `require(...)`:

- **lodash** (`4.17.21`): Full utility library (`const _ = require('lodash');`).
- **moment** (`2.30.1`): Date formatting and manipulation (`const moment = require('moment');`).
- **crypto-js** (`4.2.0`): Hashing, HMAC signing, and AES encryption (`const CryptoJS = require('crypto-js');`).
- **uuid** (`v4`): Generating UUIDs (`const { v4: uuidv4 } = require('uuid');`).
- **chai** (`4.3.10`): Assertion library backing `pm.expect`.
- **ajv** (`6.12.6`): JSON schema validation.
- **xml2js** (`0.6.2`): XML parsing and conversion.

### Unsupported Features & Importer Lint

The importer automatically scans script files and generates warnings for features that are unsupported or require attention:

- **`pm.visualizer`**: Not supported. `postctl` is a terminal client and does not render HTML visualizer webviews. Calls to `pm.visualizer` are accepted as no-ops and logged as warnings.
- **`pm.vault`**: Not supported. Postman Vault is replaced by native macOS Keychain integration.
- **`pm.require`**: Cloud-based npm package resolution is not supported. Use the built-in bundled libraries via `require('<module>')`.
- **Node.js Built-ins**: Native Node modules such as `fs`, `path`, `child_process`, and `net` are not available in the sandbox.
- **Legacy Globals**: Legacy Postman syntax (`tests["..."] = ...`, `responseBody`, `postman.getEnvironmentVariable`) is supported on a best-effort compatibility layer, but the importer flags them so they can be modernized to `pm.test` and `pm.environment.get`.

---

## 5. Newman vs `postctl run`

The linear CLI runner in `postctl` is designed as a drop-in alternative to Newman for CI/CD pipelines, automated regression runs, and terminal automation.

### Command Comparison

| Newman Command | `postctl run` Equivalent | Notes |
| --- | --- | --- |
| `newman run col.json` | `postctl run <path>` | Path is `<collection>/<folder>...` matched case-insensitively. |
| `-e env.json` | `-e <env-name>` | Uses environment name or file. If omitted, uses active environment. |
| `-d data.csv` | `-d data.csv` | Supports both CSV and JSON data files. |
| `-n 3` | `-n 3` | Number of iterations. |
| `--delay-request 200` | `--delay 200ms` | Takes Go duration strings (`200ms`, `1s`). |
| `--bail` | `--bail` | Halts execution immediately upon first assertion or request failure. |
| `--reporters cli,junit` | `--reporter cli,junit --reporter-junit-export report.xml` | Multiple reporters require an export destination for secondary reports. |
| `--reporters json` | `--json` | Emits a stream of NDJSON events for real-time parsing in CI. |
| `--timeout-request 5000` | `--timeout-request 5s` | Sets request timeout override. |
| `--suppress-exit-code` | *Not supported* | `postctl` uses standard exit codes: `0` passed, `1` failed test/script, `2` transport error, `3` usage error. |

### Key Improvements over Newman

1. **Native Single Binary**: No Node.js runtime, npm dependencies, or global package management required.
2. **Streaming NDJSON**: `--json` outputs line-delimited events (`run-started`, `request-started`, `request-finished`, `run-finished`) as they happen rather than buffering an entire run document in memory.
3. **Automatic Secret Masking**: Credentials and secrets are never printed to terminal stdout or written to JUnit reports.
4. **Shared Engine**: Runs share the exact same resolution, cookie jar, and JavaScript sandbox as the interactive TUI.
