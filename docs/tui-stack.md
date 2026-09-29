# postctl — technology and architecture

## Purpose

This document records the implementation stack for the macOS-first Go TUI described in [plan.md](plan.md), the dependency policy, and the package boundaries every workstream follows. It is adapted from the `prod-console-tui` stack document; the Charm stack and its policies are the same, and the API-client-specific choices (JavaScript runtime, syntax highlighting, JSON querying, YAML storage) are added below.

## Recommended stack

| Area | Choice | Capability and boundary | When |
| --- | --- | --- | --- |
| Language/runtime | Go 1.26 | One small native binary; the standard library covers HTTP, JSON, crypto, URL handling, process signals, and tests. | v1 |
| TUI runtime | Bubble Tea v2 (`charm.land/bubbletea/v2`) | Event loop and model/update/view architecture. Model user-visible states explicitly and run HTTP, scripts, disk, and Keychain work as asynchronous commands that return typed messages. | v1 |
| UI components | Bubbles v2 (`charm.land/bubbles/v2`) | List, table, viewport, spinner, text input, textarea, help, key bindings. Use components where their interaction model fits; keep domain state in our feature models. | v1 |
| Styling/layout | Lip Gloss v2 (`charm.land/lipgloss/v2`) | Terminal-cell-aware layout, padding, borders, alignment, styling, color-profile downsampling, light/dark-aware colors. Semantic styles live in one theme package (`internal/ui`). | v1 |
| Forms | Huh v2 (`charm.land/huh/v2`) | Focused inputs, selection, confirmation, validation, accessible mode. Used for settings and confirmations, not for navigation. | When forms are needed |
| HTTP | Go `net/http`, `net/http/httptrace`, `net/url`, `crypto/tls` | Custom transport for DNS/connect/TLS/TTFB timings, cookie jar, proxy, client certificates, redirect control, request contexts and timeouts. Views never call HTTP directly. | v1 |
| HTTP compression | `github.com/andybalholm/brotli` | Small, pure-Go Brotli decoder used alongside the standard library's gzip and deflate support. | v1 |
| Cookie policy | `golang.org/x/net/publicsuffix` | Mozilla public-suffix data for rejecting unsafe cross-site cookie domains in the persistent workspace jar. | v1 |
| JavaScript runtime | [`github.com/dop251/goja`](https://github.com/dop251/goja) (+ `goja_nodejs` for `console` and a `require` registry) | Runs Postman pre-request and test scripts with a `pm.*` API implemented in Go. Pure Go (no cgo), so `CGO_ENABLED=0` single-binary builds stay possible; supports ES5.1 plus most of ES2015+; has interrupt support for hard per-script timeouts and cancellation. Alternatives (QuickJS via cgo or wasm, V8 bindings) are faster but add cgo or a heavy runtime, which conflicts with local single-binary builds. | Script workstream |
| Syntax highlighting | [`github.com/alecthomas/chroma/v2`](https://github.com/alecthomas/chroma) | Lexers for JSON, XML, HTML, JavaScript and more. Chroma's token types are mapped onto Clear Signal `syntax.*` tokens; its built-in styles are never used, so no one-off colors enter the UI. | Viewer workstream |
| JSON query | [`github.com/tidwall/gjson`](https://github.com/tidwall/gjson) | Path filter in the response viewer (`data.items.#.id`) without unmarshalling into Go structures; tolerant of large bodies and fast on partial reads. Read-only, so it cannot mutate responses. | Viewer workstream |
| Storage | YAML files via `go.yaml.in/yaml/v3` | One YAML file per request, scripts as real `.js` files, deterministic output, strict decoding. See [model.md](model.md). | v1 |
| Secret store | macOS Keychain behind the `store.SecretStore` interface | `/usr/bin/security` invoked through `os/exec` without a shell; secrets go over stdin (`security -i`), base64-encoded, never argv. `store.MemorySecrets` is the test fake. | v1 |
| Logging | Go `log/slog` to a file outside the TUI renderer | Structured diagnostics with redaction. Never log secret values, tokens, header values, request/response bodies, or query strings. | v1 |
| WebSocket | `github.com/coder/websocket` | Context-aware client with a small API surface for WebSocket requests. | WebSocket workstream |
| Test support | Go `testing`, `net/http/httptest`, fakes behind narrow interfaces | Verify pipeline ordering, auth application, script semantics, and storage round trips without the network or Keychain. | v1 |

### Why this stack

Bubble Tea is the best fit for this app's asynchronous shape: a send runs pre-request scripts, waits on the network, runs tests, and records history, and each phase must be an explicit UI state. The model/update/view loop makes waiting, success, cancellation, and failure explicit; commands are the boundary for effects, and rendering stays a pure view of current state. Bubbles supplies the list, table, textarea, and viewport pieces; Lip Gloss handles layout across the responsive breakpoints; Huh covers forms and accessible prompting. See the [Bubble Tea project](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), [Lip Gloss](https://github.com/charmbracelet/lipgloss), and [Huh](https://github.com/charmbracelet/huh).

goja is chosen because Postman compatibility depends on running real, user-written JavaScript safely: it must be embeddable, interruptible (a runaway script must never freeze the UI), and cgo-free. Its cost is speed and partial modern-JS coverage; the compatibility corpus in the script workstream measures the gap. Chroma and gjson are small, pure-Go, widely used libraries that solve response rendering and querying without us writing lexers or path parsers.

These are recommendations inferred from the project's interaction model and the libraries' documented capabilities. They are not a claim that a framework supplies good UX by itself; navigation, wording, error recovery, narrow-terminal behavior, and accessibility remain product work owned by this application.

## Alternatives considered

| Library | Strength | Why it is not the default here |
| --- | --- | --- |
| [tview](https://github.com/rivo/tview) with tcell | Rich built-in primitives including tables, forms, lists, text views, tree views, grids, and modal windows. | The send pipeline is an asynchronous state machine; Bubble Tea's explicit model and command flow aligns more directly with it, and tview requires cross-goroutine updates to go through its queue. |
| [gocui](https://github.com/jroimartin/gocui) | Lower-level control over custom terminal layouts. | More navigation, state handling, and polished components would be application-owned. |
| [termui](https://github.com/gizak/termui) | Prebuilt dashboard widgets. | Focused on dashboards and charts, not editing and navigation. |
| Bubble Tea v1 | Well-known, broad examples. | New work targets the stable v2 import path; the v2 upgrade guides document the changes, so do not mix v1 and v2 dependencies. |
| Cobra | Rich command trees and completion. | The CLI surface is small; stdlib `flag` with a command table (as in `prod-console-tui/internal/cli`) keeps the dependency set and startup time down. |
| Nodejs/newman as script runtime | Perfect Postman compatibility. | Requires a Node install and process boundary; violates the single-binary, offline goals. |

## Application architecture

Keep framework code at the edge. Feature services are testable without a terminal; the TUI contains no HTTP, script, storage, or Keychain policy.

```text
cmd/postctl
  entry, flags, wiring, version (-X main.version)

internal/model
  pure data: Workspace, Collection, Folder, Request, Body, Auth, Variable,
  Environment, Script (no I/O)
internal/store
  YAML/JS load and save, atomic writes, migrations, SecretStore + Keychain
internal/importer/postman        (Postman v2.0/v2.1 -> model)
internal/importer/postmanlocal   (local Postman install -> v2.1 JSON staging)
internal/vars                    ({{var}} / {{$dynamic}} scopes and resolution)
internal/httpx                   (request builder, auth, transport with timings, cookies)
internal/script                  (goja sandbox, pm.* API, console capture)
internal/engine                  (send pipeline, collection runner, history) - NO TUI imports
internal/tui                     (root model, panes, screens, modals, palette, editors)
internal/ui                      (theme tokens, shared components)
internal/cli                     (linear commands: import, ls, send, run, env)
```

The tree may be adjusted as workstreams land. Avoid splitting every screen into a package prematurely: feature boundaries follow behavior and ownership.

### State and effects

- The root model owns route and shared app context: workspace, active environment, focused pane, open requests.
- Each feature owns a small explicit state machine. A send moves through: idle, resolving, pre-request script, sending, receiving, test script, done, failed, cancelled.
- `Update` handles user input and typed result messages. Commands perform HTTP, scripts, disk, Keychain, and timers, and return result messages; they never mutate a screen directly.
- Views render existing state only. A view must not do HTTP, run scripts, touch the disk or Keychain, or mutate app state.
- Use context cancellation and bounded timeouts for network and script work; scripts run with a hard timeout and an interrupt so a runaway script never freezes the UI.
- Centralize key bindings and provide a visible `?` help view. Keep standard navigation available without a mouse.
- Put storage, secrets, HTTP, and script execution behind narrow interfaces so state transitions can use fakes.
- `internal/engine` has zero TUI imports; the TUI and the CLI are both thin clients over it.

### Storage and secret rules

- The workspace is a directory tree of YAML and `.js` files (see [model.md](model.md)); it is designed to live in git. Local, non-versioned state (history, cookies, active environment) lives under `.postctl/`, which is gitignored.
- Secret variable values and OAuth 2.0 tokens live only in the Keychain; files hold a `secretRef`. Loading a workspace never reads the Keychain; secrets are hydrated on demand.
- Secrets never appear in URLs shown in errors, logs, history files, the command palette, or crash text. Redact before display, not after.
- If the Keychain is unavailable, do not silently fall back to plaintext files. Explain the limitation and allow a process-only (in-memory) session.
- Writes are atomic (temp file plus rename) and unchanged files are not rewritten, so autosave is cheap and git diffs stay minimal.

## Versioning and dependency policy

- Use the stable v2 module paths: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`, and `charm.land/huh/v2`.
- Pin compatible releases together in `go.mod`; update them as a set after reviewing each upstream upgrade guide. Bubble Tea v2, Bubbles v2, and Lip Gloss v2 introduced coordinated import/API changes. See the [Bubbles v2 upgrade guide](https://github.com/charmbracelet/bubbles/blob/main/UPGRADE_GUIDE_V2.md) and [Lip Gloss v2 upgrade guide](https://github.com/charmbracelet/lipgloss/blob/main/UPGRADE_GUIDE_V2.md).
- Prefer the Go standard library until a real requirement justifies a third-party dependency. Each new dependency needs a reason in this document (what it does, why not stdlib, why this library).
- Third-party dependencies must be pure Go (no cgo) so `CGO_ENABLED=0` builds keep working.
- Keychain access uses `/usr/bin/security` via `os/exec` (argument-safe, no shell). `zalando/go-keyring` was considered; it passes secrets on the command line, which exposes them in the process list, so it is not used.
- The YAML library is `go.yaml.in/yaml/v3` (the maintained fork of `gopkg.in/yaml.v3`).

## Validation and build workflow

`task build test lint` (or `go build ./... && go test -race ./... && golangci-lint run`) must pass before a workstream is done. Tests are table-driven and use fakes: `store.MemorySecrets` for the Keychain, `httptest` servers for HTTP, temp directories for storage. Golden files under `internal/store/testdata/golden/` pin the on-disk format; regenerate deliberately with `go test ./internal/store -run TestGolden -update` and review the diff. Tests that need real user data read only from the gitignored `testdata/private/` and skip when it is absent.

Initial support is macOS only (arm64 and amd64); keep OS-specific code behind interfaces anyway. Distribution is local builds only for now (`go install ./cmd/postctl`, with `-X main.version=...` for a version string); there is no release toolchain yet.

## Upstream references

- [Bubble Tea README, tutorial, and releases](https://github.com/charmbracelet/bubbletea)
- [Bubbles components and v2 migration guide](https://github.com/charmbracelet/bubbles)
- [Lip Gloss styling and color behavior](https://github.com/charmbracelet/lipgloss)
- [Huh forms and accessible mode](https://github.com/charmbracelet/huh)
- [goja](https://github.com/dop251/goja) and [goja_nodejs](https://github.com/dop251/goja_nodejs)
- [Chroma](https://github.com/alecthomas/chroma)
- [gjson](https://github.com/tidwall/gjson)
- [coder/websocket](https://github.com/coder/websocket)
- [tview](https://github.com/rivo/tview), [gocui](https://github.com/jroimartin/gocui), [termui](https://github.com/gizak/termui)
