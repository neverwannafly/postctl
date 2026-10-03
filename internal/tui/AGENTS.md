# internal/tui — local rules

Read the root [`AGENTS.md`](../../AGENTS.md) and [`docs/design-language.md`](../../docs/design-language.md) first. This tree is the Bubble Tea v2 shell (`internal/tui`) and its panes and screens (subpackages). Colors, glyphs and shared components come from `internal/ui`.

## Invariants

- **`View` is pure.** No I/O, no mutation, not even scroll offsets or cursor fixes: do that in `Update` or `SetSize`. Past offenders: `envedit`, `cookies`, textareas in `request` and `wspane`. Each has a purity test; copy it (`TestViewIsPure`, `TestShellViewIsPure`, `TestTextareaViewIsPure`).
- **Effects are `tea.Cmd`s** that return typed messages: disk (`Backend`), Keychain, network, `$EDITOR`, pager, clipboard. Panes edit the live workspace on the Update goroutine; slow work runs on `Clone()` snapshots inside a command. A pane that saves goes through `tui.SaveMsg` (debounced, flushed on blur via `BlurCmd`), never straight to disk.
- **Sending** goes only through `tui.Sender` (`send.go`); the real adapter is `internal/tui/tuiengine`. Panes never import `internal/engine`; only the shell's `runner.go`, `runner/`, `fakerun/` and `tuiengine/` use engine types (runner events, outcomes). Additions to `send.go` are additive (plan C2).
- **Secrets** are masked (`ui` glyph `Mask`: `••••`, ASCII `****`) in every frame, console line, test message, Timeline URL, header, pager input and export, unless the user reveals; reveal resets on move. Reuse `history.Redactor` (`response.MaskBodyForExternalOutput` for external output). Sentinel-checked by `internal/qa`.
- **Design language**: semantic tokens only; never color alone; every empty, error and waiting state names the next action; usable at 80x24; vim keys per the design-language key map (plan U1), mode visible in the footer. Only valid key hints in the footer.
- **Do not ship no-ops.** A key, palette action or help line must work. Optional features are `Options` factories; nil hides them (`NewRunner`, `NewHistory`, `NewEnvEditor`, `NewCookieManager`, `NewPaneForKind`).
- WebSocket requests use `wspane` through `kindrouter.go`; non-key messages reach every pane in the Request slot, so a hidden pane still gets its own results.

## Key files

| File | Role |
| --- | --- |
| `app.go` | root model, `Options`, phases, reload, watcher hookup |
| `pane.go` | `Pane`, `Screen`, `Registry`/`Action`, `PaneEnv`, `OpenScreenMsg`, selection and reload messages |
| `backend.go`, `save.go` | `Backend` over `store`; serialized, coalescing save queue |
| `send.go`, `sendrun.go` | send contract; per-request send flights, cancel, retry |
| `keys.go`, `help.go`, `palette.go` | global key map (`newKeyMap`), help overlay, command palette |
| `layout.go`, `sidebar.go`, `tree.go` | responsive layout (`ui.LayoutFor`), collection tree |
| `watch.go` | fsnotify reload; ignores its own saves for a window; polling fallback |
| `request/`, `response/`, `wspane/` | request editor, response viewer, WebSocket pane |
| `envedit/`, `cookies/`, `runner/`, `history/` | screens (Ctrl-E, palette, `R`, `H`) |
| `tuitest/`, `fakesend/`, `fakerun/`, `testdata/demo` | synthetic workspaces and fakes |

## Keys

Global bindings are `keyMap` in `keys.go`; each pane has its own `keys.go`. The reference `docs/keys.md` is generated from the hand-kept `KeyCatalog` in `cmd/genkeys/main.go`: add the key there, run `task keys`, commit the diff. `TestGeneratedKeysDocMatchesFile` and `TestAllCodeKeyBindingsDocumentedInKeysDoc` fail on drift. `Ctrl-S` always sends; `Ctrl-Enter` needs enhanced keyboard reporting.

## Tests

```sh
go test -race ./internal/tui/...                 # ~30 s; tui and wspane are the slow ones
go test ./internal/tui/request -update           # goldens per package (see root AGENTS.md)
task tui:smoke                                   # real binary through a pty, fake sender, offline
```

- Goldens: three sizes (80x24, 120x40, 200x60) by three looks (dark, `NO_COLOR`, ASCII). Review the text of every changed golden, not only that it passes.
- Shell tests use `fakeBackend` (`helpers_test.go`, records every write) and `tuitest.Demo()`. Pane tests drive `Update` and `View` directly with fake clocks; add a "fits its size" assertion for new tabs.
- Hand-run: `POSTCTL_FAKE_SENDER=1 go run ./cmd/postctl --workspace <copy of internal/tui/testdata/demo>`; never edit the committed demo in place.

## Pitfalls

- Reloads: the watcher reloads files changed elsewhere; dirty edits in panes must survive it (selection and scroll are preserved across sends, resizes and reloads).
- `Esc` cancels a running send or OAuth flow; keep that working in any new pane.
- Empty Headers or Timeline states and OAuth errors were rejected once for missing a next action.
- `internal/tui/tuiengine/tuiengine.go` says the TUI "never imports the engine"; the accurate rule is that only the shell's runner contract and the adapters do.
