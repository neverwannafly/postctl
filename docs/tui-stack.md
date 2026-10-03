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
| Forms | none yet (Huh v2, `charm.land/huh/v2`, was considered) | Not a dependency today: settings and confirmations are built from Bubbles and our own dialogs. Add Huh only with a reason recorded here. | Not used |
| HTTP | Go `net/http`, `net/http/httptrace`, `net/url`, `crypto/tls` | Custom transport for DNS/connect/TLS/TTFB timings, cookie jar, proxy, client certificates, redirect control, request contexts and timeouts. Views never call HTTP directly. | v1 |
| HTTP compression | `github.com/andybalholm/brotli` | Small, pure-Go Brotli decoder used alongside the standard library's gzip and deflate support. | v1 |
| Cookie policy | `golang.org/x/net/publicsuffix` | Mozilla public-suffix data for rejecting unsafe cross-site cookie domains in the persistent workspace jar. | v1 |
| JavaScript runtime | [`github.com/dop251/goja`](https://github.com/dop251/goja) (+ `goja_nodejs` for `console` and a `require` registry) | Runs Postman pre-request and test scripts with a `pm.*` API implemented in Go. Pure Go (no cgo), so `CGO_ENABLED=0` single-binary builds stay possible; supports ES5.1 plus most of ES2015+; has interrupt support for hard per-script timeouts and cancellation. Alternatives (QuickJS via cgo or wasm, V8 bindings) are faster but add cgo or a heavy runtime, which conflicts with local single-binary builds. | Script workstream |
| Syntax highlighting | [`github.com/alecthomas/chroma/v2`](https://github.com/alecthomas/chroma) | Lexers for JSON, XML, HTML, JavaScript and more. Chroma's token types are mapped onto Clear Signal `syntax.*` tokens; its built-in styles are never used, so no one-off colors enter the UI. | Viewer workstream |
| JSON query | [`github.com/tidwall/gjson`](https://github.com/tidwall/gjson) | Path filter in the response viewer (`data.items.#.id`) without unmarshalling into Go structures; tolerant of large bodies and fast on partial reads. Read-only, so it cannot mutate responses. | Viewer workstream |
| jq filter | [`github.com/itchyny/gojq`](https://github.com/itchyny/gojq) (v0.12.19) | `Space j` in the response pane runs a jq program over the (masked) JSON body: pure Go, no cgo. Used only through `internal/tui/response/jq.go`: parse, compile with no module loader and an empty environment (`$ENV` and `env` see nothing), evaluate under a 250 ms context timeout and a 1 MiB output cap, and run inside a `tea.Cmd`, never in `View`. Runtime errors are shown without echoing data values. | Response V3 |
| Storage | YAML files via `go.yaml.in/yaml/v3` | One YAML file per request, scripts as real `.js` files, deterministic output, strict decoding. See [model.md](model.md). | v1 |
| Secret store | macOS Keychain behind the `store.SecretStore` interface | `/usr/bin/security` invoked through `os/exec` without a shell; secrets go over stdin (`security -i`), base64-encoded, never argv. `store.MemorySecrets` is the test fake. | v1 |
| Logging | none yet (Go `log/slog` to a file outside the renderer is the intended design) | No diagnostics log exists today; nothing in the code imports `log/slog`. If one is added: structured, redacted, never secret values, tokens, header values, bodies or query strings. | Not built |
| WebSocket | `github.com/coder/websocket` (v1.8.14) | Pure-Go, context-aware client with a small API: `Dial` with headers, an `http.Client` (cookie jar, TLS settings, proxy), subprotocols, ping/pong hooks, close codes, and a read limit. `internal/wsx` wraps it; nothing else imports it. The standard library has no WebSocket client, and `golang.org/x/net/websocket` is deprecated and lacks context support, close codes and ping hooks. | v1 (WS9) |
| Test support | Go `testing`, `net/http/httptest`, fakes behind narrow interfaces | Verify pipeline ordering, auth application, script semantics, and storage round trips without the network or Keychain. | v1 |

### Why this stack

Bubble Tea is the best fit for this app's asynchronous shape: a send runs pre-request scripts, waits on the network, runs tests, and records history, and each phase must be an explicit UI state. The model/update/view loop makes waiting, success, cancellation, and failure explicit; commands are the boundary for effects, and rendering stays a pure view of current state. Bubbles supplies the list, table, textarea, and viewport pieces; Lip Gloss handles layout across the responsive breakpoints; Huh covers forms and accessible prompting. See the [Bubble Tea project](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), [Lip Gloss](https://github.com/charmbracelet/lipgloss), and [Huh](https://github.com/charmbracelet/huh).

goja is chosen because Postman compatibility depends on running real, user-written JavaScript safely: it must be embeddable, interruptible (a runaway script must never freeze the UI), and cgo-free. postctl also imports [`github.com/dlclark/regexp2/v2`](https://github.com/dlclark/regexp2) (the backtracking engine goja already uses for lookahead and backreferences) directly, only to set its match timeout so a catastrophic pattern cannot hold a script past its deadline. Its cost is speed and partial modern-JS coverage; the compatibility corpus in the script workstream measures the gap. Chroma and gjson are small, pure-Go, widely used libraries that solve response rendering and querying without us writing lexers or path parsers.

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

### vimtext

`internal/tui/vimtext` is postctl's reusable modal editor for single-line fields,
multi-line scripts and bodies, prompts, and read-only response text. It is
custom instead of wrapping Bubbles `textarea` because every field needs the
same Vim operators and text objects, parent key routing, redacted OSC 52 yanks,
completion, syntax spans, grapheme-safe movement, and pure cached rendering.
It adds no dependency: Chroma, Lip Gloss, `x/ansi`, and `uniseg` were already
in the module graph. The supported bindings are the **Text fields** rows in
the [design-language key map](design-language.md#key-map).

Two small packages sit on top of it for the request and WebSocket panes.
`internal/tui/vimfield` wraps one editor as a pane field (open in normal or
insert mode, fit to a size, paste, a pure copy-based `Render`, and `Popup` /
`Overlay` so a one-line field can show its completion menu over the rows
below). `internal/tui/complete` supplies the completers: `Vars` lists `{{var}}`
and `{{$dynamic}}` names from the scope chain with the nearest layer winning
and secrets shown as the mask glyph only, and `PM` lists `pm.*` members from
one static table in `pm.go` that a test checks against `docs/scripting.md`.

Vim semantics are checked by the opt-in `TestDifferentialNvim` oracle. Set
`VIMTEXT_NVIM=1` to compare the eligible keystroke corpus and 500 seeded,
3–12-key generated cases with `nvim --clean --headless -n`. Each case runs in
an isolated process with a per-case timeout, cleared registers and search
state, and the original keys fed in typed mode; text, grapheme cursor position,
and final mode are compared. The harness pins corpus inclusion and exclusion
counts, asserts generated command/operator coverage, and uses two-space
`expandtab` settings to match `Options.TabText`'s documented default.
The sole intentional compatibility deviation is search syntax: vimtext treats
the pattern literally, while Nvim treats it as a regular expression. The
allowlist is kept in `internal/tui/vimtext/deviations.go`.

### Storage and secret rules

- The workspace is a directory tree of YAML and `.js` files (see [model.md](model.md)); it is designed to live in git. Local, non-versioned state (history, cookies, active environment) lives under `.postctl/`, which is gitignored.
- Secret variable values and OAuth 2.0 tokens live only in the Keychain; files hold a `secretRef`. Loading a workspace never reads the Keychain; secrets are hydrated on demand.
- Secrets never appear in URLs shown in errors, logs, history files, the command palette, or crash text. Redact before display, not after.
- If the Keychain is unavailable, do not silently fall back to plaintext files. Explain the limitation and allow a process-only (in-memory) session.
- Writes are atomic (temp file plus rename) and unchanged files are not rewritten, so autosave is cheap and git diffs stay minimal.

### Request pane, environment editor and cookie manager

`internal/tui/request` (Request pane), `internal/tui/envedit` (Ctrl-E, palette "Edit environments") and `internal/tui/cookies` (palette "Cookies") plug in through `Options.NewRequestPane`, `NewEnvEditor` and `NewCookieManager`; `cmd/postctl/wire.go` fills them in. Panes edit the live workspace on the Update goroutine and persist through `tui.SaveMsg` (debounced 500 ms, flushed on blur, selection change and close). Anything slow (curl build, OAuth, secret reads, `$EDITOR`) runs in a `tea.Cmd` on snapshots.

Request pane keys. Every text field (URL bar, table cells, form rows, bodies, scripts) is a `vimtext` editor wrapped by `internal/tui/vimfield`; the footer shows its mode first. The full list is in [keys.md](keys.md).

| Where | Keys |
| --- | --- |
| Anywhere in the pane | `Ctrl-S` send (also `Ctrl-Enter` with an enhanced keyboard; works in insert, normal and visual mode), `Ctrl-Z` / `Ctrl-Y` undo / redo of the request, `u` / `Ctrl-r` the same when no field is open, `Space o` open in `$EDITOR` (URL, cell, form row, body or script), `gu` focus the URL, `Esc` cancel send or OAuth once no field is open |
| Text fields | `Enter` opens a field in normal mode, `i` `a` `I` `A` open it typing; `Esc` insert to normal, normal to leaving the field. `Space`, `:`, `q`, `g`+letter, `[` `]` in normal mode go to the shell (`tui.ForwardKeyMsg`, `tui.ForwardSeqMsg`); in insert and visual mode they are text. `{{` completes variables (masked secrets), `pm.` completes the script API |
| URL bar | `Enter` send (normal and insert), `Shift-Tab` method, `Tab` / `Down` to the tabs |
| Tab row | `←` `→` switch tab, `↑` URL, `↓` / `Enter` into the tab, `m` method, `Space y` copy as curl (secrets masked), `Space Y` copy with secrets |
| Key-value tables | `a` add, `Enter` / `i` edit, `Tab` / `Shift-Tab` next / previous field, `x` toggle, `d` delete, `Alt-↑` `Alt-↓` (or `Shift`) move, `b` bulk edit, `t` text / file (form-data) |
| Body / Scripts / Auth | `f` format JSON; `Space i` snippets; `Space v` reveal secrets (`Ctrl-R` while typing), `g` get OAuth token, `c` copy sign-in link |

A one-line field has no rows to spare, so its completion menu is drawn by the owner over the neighbouring rows (`vimfield.Field.Popup`, `vimfield.Overlay`). Each visit to a field is one request-level undo step (`recordIn`); inside the field the editor has its own undo. The old `internal/tui/textin` package and the Bubbles `textarea` wrappers are gone: the WebSocket log search and template rename prompts are one-line `vimfield`s too (Enter keeps or saves, `Esc` in insert mode only returns to normal mode, `Esc` in normal mode clears or cancels). `Space o` opens the WebSocket URL, a cell, a form row or the composer in `$EDITOR`, like the request pane.

Environment editor: `[` `]` or `1` `2` `3` switch Environments / Globals / Collections. List: `n` new, `r` rename, `c` duplicate, `d` delete (asks, defaults to Cancel), `p` production, `Enter` / `Tab` to the variables, `Esc` close. Variables: `a` add, `Enter` edit, `Space` enable, `d` delete, `Alt-↑` `Alt-↓` move, `s` toggle secret (secret to plain asks first), `r` reveal (reads the Keychain in a command), `Esc` back.

Cookie manager: `↑` `↓` `PgUp` `PgDn` `Home` `End` move, `r` reveal value, `d` delete cookie, `x` clear domain and `X` clear all (both ask, default Cancel), `Esc` close.

## Versioning and dependency policy

- Use the stable v2 module paths: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, and `charm.land/lipgloss/v2` (`charm.land/huh/v2` if Huh is ever adopted).
- Pin compatible releases together in `go.mod`; update them as a set after reviewing each upstream upgrade guide. Bubble Tea v2, Bubbles v2, and Lip Gloss v2 introduced coordinated import/API changes. See the [Bubbles v2 upgrade guide](https://github.com/charmbracelet/bubbles/blob/main/UPGRADE_GUIDE_V2.md) and [Lip Gloss v2 upgrade guide](https://github.com/charmbracelet/lipgloss/blob/main/UPGRADE_GUIDE_V2.md).
- Prefer the Go standard library until a real requirement justifies a third-party dependency. Each new dependency needs a reason in this document (what it does, why not stdlib, why this library).
- Third-party dependencies must be pure Go (no cgo) so `CGO_ENABLED=0` builds keep working.
- Keychain access uses `/usr/bin/security` via `os/exec` (argument-safe, no shell). `zalando/go-keyring` was considered; it passes secrets on the command line, which exposes them in the process list, so it is not used.
- The YAML library is `go.yaml.in/yaml/v3` (the maintained fork of `gopkg.in/yaml.v3`).
- `postctl env set <env> <key> --secret` without a value prompts with echo off through `github.com/charmbracelet/x/term` (`ReadPassword`). It is already in the module graph as a dependency of Bubble Tea and is pure Go, so it adds no module; it is a direct dependency now. The standard library cannot turn off terminal echo, and `golang.org/x/term` would be a second module for the same job.
- File watching uses `github.com/fsnotify/fsnotify` (pure Go). It reloads the workspace when files change outside postctl (editor, `git pull`). The standard library has no change notification; polling every directory would cost battery and latency. The shell debounces events (250 ms), ignores dot directories and its own saves, and falls back to a 2 s polling watcher if the OS cannot watch the whole tree (kqueue holds one descriptor per file).

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
- [gojq](https://github.com/itchyny/gojq)
- [coder/websocket](https://github.com/coder/websocket)
- [tview](https://github.com/rivo/tview), [gocui](https://github.com/jroimartin/gocui), [termui](https://github.com/gizak/termui)
