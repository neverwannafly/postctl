# postctl

A keyboard-first terminal UI and scriptable CLI for API development that replaces Postman: browse collections and folders, edit requests, send them, inspect responses, execute pre-request and test scripts with Chai assertions, and run automated multi-step collections—stored as clean, versionable YAML and `.js` files that live in git right next to your code.

## Install

Requires Go 1.26 or newer on macOS (Apple Silicon or Intel).

```sh
go install ./cmd/postctl
```

The binary installs to `$(go env GOPATH)/bin` (by default `~/go/bin`). Ensure that directory is on your `PATH`:

```sh
# For zsh (~/.zshrc):
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc

postctl --version
```

## Quick Start

### 1. Import your Postman data

If you already use Postman Desktop on macOS, import your cached collections, environments, and globals directly without manual exports:

```sh
# Preview collections and environments found in local Postman cache
postctl import postman-local --dry-run

# Import into current directory
postctl import postman-local
```

> **Note on local extraction:** `postctl import postman-local` reads the local Chromium IndexedDB cache from Postman Desktop in read-only mode from a temporary copy. If a collection was not cached locally by the desktop app, fall back to Postman's **File → Export** to produce a Collection v2.1 JSON file.

Alternatively, import standard Postman Collection v2.0 or v2.1 JSON files:

```sh
postctl import postman ~/Downloads/MyCollection.json \
  --env ~/Downloads/Staging.postman_environment.json \
  --globals ~/Downloads/workspace.postman_globals.json
```

Sensitive credentials (bearer tokens, passwords, API keys) are automatically extracted to your macOS Keychain during import ([D2 secrets migration](docs/migrating-from-postman.md#3-secrets-migration-decision-d2)).

### 2. Launch the TUI

```sh
postctl
```

Select any request in the sidebar using `↑` and `↓`, press `Ctrl-Enter` (or `Ctrl-S`) to send, and inspect the formatted response, headers, timing timeline, and test results in the response viewer.

To explore the committed sample workspace:

```sh
postctl --workspace examples/httpbin
```

---

## Where Data Lives

A `postctl` workspace is a clean directory tree designed to be committed to version control:

```text
my-service/
  postctl.yaml                      # Workspace manifest and defaults
  globals.yaml                      # Workspace-level variables
  environments/                     # Environment files
    staging.yaml
    production.yaml
  collections/                      # Collections and folders
    payments/
      collection.yaml               # Child order, collection auth, collection variables
      pre.js  test.js               # Collection-level scripts
      001-list-charges.yaml         # Request definition
      001-list-charges.test.js      # Sibling script file
  .postctl/                         # Local unversioned state (gitignored)
    state.yaml                      # Active environment and folded tree state
    cookies.json                    # Workspace cookie jar (mode 0600)
    history/                        # Append-only request log
```

- **Workspace discovery:** `postctl` automatically discovers workspaces by walking up the directory hierarchy from your current working directory searching for `postctl.yaml`.
- **Explicit workspace:** Pass `--workspace <dir>` or set `POSTCTL_WORKSPACE=<dir>` to target a workspace explicitly.
- **Global fallback:** If no local workspace is found, `postctl` defaults to `~/.config/postctl` (customizable via `POSTCTL_HOME`).
- **Gitignored state:** Local session state lives in `.postctl/` (active environment selection, response history, cookies). Keep `.postctl/` in `.gitignore`.
- **Secrets in Keychain:** Variables marked as secret (`type: secret`) store only an opaque reference (`secretRef`) on disk. Secret values are stored securely in the macOS Keychain (`/usr/bin/security`) and hydrated into memory only when sending requests.

---

## TUI Keyboard Shortcuts

`postctl` is keyboard-first and vim-flavoured: motions move through lists and panes, actions sit behind the `Space` leader, and arrows, `Enter` and `Esc` keep working everywhere. Press `Space` and wait 300 ms to see the keys that are available right now (the which-key popup).

| Key | Action |
| --- | --- |
| `Ctrl-S` / `Ctrl-Enter` | **Send request** immediately (Ctrl-S always works, even while typing) |
| `Space` | **Leader**: `Space f` (or `Space Space`) find a request, `Space e` environment, `Space E` edit environments, `Space a` new, `Space r` rename, `Space d` delete, `Space D` duplicate, `Space R` run collection, `Space h` history, `Space y` copy as curl, `Space o` open in `$EDITOR`, `Space l` reload, `Space q` quit |
| `Ctrl-P` / `:` | Open the **Command Palette** / the command line (`:q`, `:q!`, `:w`, `:wq`) |
| `j` `k` `h` `l`, arrows | Move in the tree and fold folders; `Ctrl-d` / `Ctrl-u` half page, `gg` / `G` top and bottom |
| `Tab` / `Shift-Tab`, `1` `2` `3` | Move focus between panes: **Collections → Request → Response** (the pane titles show `[1] [2] [3]`) |
| `Ctrl-h` `Ctrl-j` `Ctrl-k` `Ctrl-l` | Move to the neighbouring pane (past the edge, hand the move to tmux-style hosts) |
| `g` then `u` `p` `a` `h` `b` `s` `t` `S` `r` | Go to the URL, Params, Auth, Headers, Body, Pre-request, Tests, Settings or Response body; `[` / `]` step through tabs |
| `Ctrl-o` / `Space [` / `Space ]` | Back and forward through the requests you opened |
| `Enter` | Open selected request, fold a folder, or edit a table row |
| `/` | Filter requests in sidebar, or search text in response pane |
| `Ctrl-B` | Show or hide the Collections sidebar |
| `?` | Show in-app keyboard shortcut and help overlay |
| `Esc` | Leave a text field, close an overlay, then cancel a running send |
| `Ctrl-C` twice / `q` | Quit `postctl` (`Ctrl-C` in a text field acts as `Esc`) |

Typing in a text field always inserts text: `Space`, `g`, `q` and `:` never start a shortcut there. See [`docs/keys.md`](docs/keys.md) for the complete reference across all panes and dialogs.

---

## CLI Commands

`postctl` includes a scriptable, non-interactive CLI sharing the same engine as the TUI:

```sh
# List collections and requests (indented tree or flat paths for fzf)
postctl ls
postctl ls --flat
postctl ls Payments/Billing --json
postctl ls --workspace ./my-api

# Send a single request from the terminal
postctl send Payments/Billing/Create -e staging --var amount=100
postctl send Payments/List\ charges --include --verbose

# Run collections and folders in CI (Newman replacement)
postctl run Payments -e staging --var region=local --bail
postctl run Payments -d data.csv --reporter cli,junit --reporter-junit-export junit.xml
postctl run Payments --json

# Manage environments and secrets
postctl env ls
postctl env use staging
postctl env show staging --reveal
postctl env set staging token --secret   # prompts with no echo; keeps it out of shell history

# Generate curl commands with masked or revealed credentials
postctl curl Payments/List\ charges -e staging
postctl curl Payments/List\ charges --with-secrets
```

See [`docs/cli.md`](docs/cli.md) for flag details, exit codes (`0` passed, `1` failed test, `2` transport error, `3` usage error), and CI integration patterns.

---

## Scripting (`pm.*`)

`postctl` executes pre-request and test scripts in a pure-Go [goja](https://github.com/dop251/goja) ECMAScript sandbox without Node.js dependencies:

```javascript
// Pre-request script (calculating HMAC signature)
const CryptoJS = require('crypto-js');
const timestamp = Math.floor(Date.now() / 1000).toString();
const secret = pm.environment.get('signingSecret');
const signature = CryptoJS.HmacSHA256(pm.request.url.getPath() + ':' + timestamp, secret).toString(CryptoJS.enc.Hex);

pm.request.headers.upsert({ key: 'X-Timestamp', value: timestamp });
pm.request.headers.upsert({ key: 'X-Signature', value: signature });

// Test script (Chai assertions & flow control)
pm.test("Status is 200 OK", function () {
  pm.response.to.have.status(200);
});

pm.test("Response contains charge id", function () {
  const json = pm.response.json();
  pm.expect(json).to.have.property("id");
});

// Control runner sequence
pm.execution.setNextRequest("Next Request Name");
```

- **Supported scopes:** `pm.environment`, `pm.collectionVariables`, `pm.globals`, `pm.variables`, `pm.iterationData`.
- **Bundled npm libraries:** `lodash` (4.17.21), `moment` (2.30.1), `crypto-js` (4.2.0), `uuid` (8.3.2), `chai` (4.4.1), `ajv` (6.12.6), `xml2js` (0.6.2).
- **Flow control:** `pm.execution.skipRequest()` and `pm.execution.setNextRequest(name | null)`.

See [`docs/scripting.md`](docs/scripting.md) for the complete API contract.

---

## Accessibility and Terminal Modes

For Ghostty, tmux, **Ctrl-Enter**, pane handoff with **Ctrl-h/j/k/l**, and clipboard behaviour, see [`docs/terminal.md`](docs/terminal.md).

`postctl` adheres to the Clear Signal design language and adapts to all terminal configurations:

- `--accessible` (or `POSTCTL_ACCESSIBLE=1`): Emits a single-column, linear layout without alternate screen buffers, animations, or cursor resets. Suitable for screen readers.
- `--ascii` (or `POSTCTL_ASCII=1`): Replaces Unicode symbols (`●`, `✓`, `×`, `→`) with plain ASCII characters (`*`, `+`, `x`, `->`), and masks secrets with `****` instead of `••••`.
- `NO_COLOR`: Disables all ANSI colors while preserving layout hierarchy through text labels, brackets, and prefixes.
- **Narrow terminals:** Responsive breakpoints automatically reorganize panes down to 80×24.

---

## Troubleshooting

- **macOS Keychain Prompts:** When reading or setting secret variables for the first time, macOS may display a system security dialog asking to allow access to the Keychain. Click "Always Allow" or "Allow".
- **Unresolved Variables:** If a template variable `{{name}}` cannot be found in the local, data, environment, collection, or global scopes, `postctl` leaves the raw template string in place and surfaces an engine warning badge (`! Unresolved variable {{...}}`).
- **OAuth2 Loopback URIs:** For Authorization Code grants with PKCE, configure your OAuth identity provider to accept loopback redirects at `http://127.0.0.1:<port>/callback`.
- **`Ctrl-Enter` Not Triggering Send:** Some terminal emulators intercept `Ctrl-Enter` or do not emit enhanced CSI sequences. Use `Ctrl-S` (which always sends), or press `Enter` while focused on the URL bar.
- **`Ctrl-S` Freezes Terminal:** If pressing `Ctrl-S` freezes your terminal output, software flow control (XON/XOFF) is active. Disable it by running `stty -ixon` in your shell profile (`~/.zshrc` or `~/.bashrc`).

---

## Development

`postctl` is written in Go 1.26 using Charm v2 modules (`charm.land/bubbletea/v2`, `lipgloss/v2`, `bubbles/v2`).

New contributors should start with [`CONTRIBUTING.md`](CONTRIBUTING.md), follow
the guided first hour in [`docs/onboarding.md`](docs/onboarding.md), and use the
[`architecture guide`](docs/architecture.md) plus its end-to-end flow
walkthroughs. The complete documentation index is [`docs/README.md`](docs/README.md).

```sh
# Run tests and linter
go build ./...
go test -race ./...
golangci-lint run

# Or using Taskfile:
task build test lint

# Regenerate embedded JavaScript bundles (lodash, crypto-js, etc.)
task assets

# Regenerate docs/keys.md reference from code
go generate ./cmd/genkeys
```

### Repository Layout

- `cmd/postctl`: Application entry point, CLI flag parsing, and engine wiring
- `cmd/genkeys`: Generator for the complete keyboard reference in `docs/keys.md`
- `internal/model`: Pure domain data models (Workspace, Collection, Folder, Request, Auth, Variable)
- `internal/store`: YAML/JS serialization, atomic file updates, discovery, Keychain `SecretStore`
- `internal/importer`: Postman v2.x JSON importer and Desktop Chromium IndexedDB extractor
- `internal/engine`: Send pipeline, script execution stages, collection runner, history logger
- `internal/script`: goja ECMAScript sandbox, `pm.*` runtime shims, Chai assertions
- `internal/httpx`: HTTP transport, connection timing metrics, cookie jar, curl generator
- `internal/tui`: Bubble Tea v2 terminal interface (screens, sidebar, request editor, response viewer)
- `internal/ui`: Clear Signal theme tokens, color palettes, and shared UI components
- `internal/cli`: Scriptable CLI commands (`ls`, `send`, `run`, `env`, `curl`) and reporters
- `examples/httpbin`: Committed sample workspace demonstrating all major features
- `docs/`: Technical specifications, architecture records, and contributor references ([Index](docs/README.md))
