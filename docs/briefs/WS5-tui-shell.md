# WS5 — UI components and TUI shell

Owner model: Sonnet 5.5 (high) · Branch: `ws/5-tui-shell` · Worktree: `~/work/postctl-wt/ws5-tui-shell`

## Goal

The frame the whole product lives in: shared components, a responsive layout, focus and navigation, the collection tree, the environment switcher, the command palette, help, and first-run and error states. It should be real enough that `postctl` (no args) opens the user's workspace, browses collections and switches environments. Sending requests, the full request editor and the response viewer come later (WS6), but you build the panes and slots they plug into.

This is the most design-sensitive workstream. "Surreal dev experience" = instant, calm, obvious, never janky.

## Read first

- `docs/design-language.md` (binding; this repo's adapted Clear Signal), `docs/tui-stack.md`, `AGENTS.md`.
- `docs/plan.md` §0 (Q5, Q9, Q10, Q11, D1, D3), §3, §8.
- `internal/ui/theme.go` (tokens, `LayoutFor`, `Theme`, `Glyphs`): build on it; don't fork it.
- `internal/model`, `internal/store` (`Discover`, `Open`, `Load`; WS0.1 is adding `LoadLenient` in parallel. Code against it as `LoadLenient(ctx?) (*model.Workspace, []*store.FileError, error)` and fall back to `Load` behind a tiny adapter until it lands).
- Reference implementation style: `~/work/prod-console-tui/internal/tui/*.go` and `internal/ui/components.go` (Bubble Tea v2 patterns, msgs, keys, modal, list, help). Match its quality and idioms: explicit states, effects as `tea.Cmd`, views pure.

## Scope (files you own)

`internal/ui/` (components; tokens may gain additive entries), `internal/tui/`, and in `cmd/postctl` only the no-args → TUI launch plus global flags `--workspace`, `--accessible`, `--ascii` (honor `NO_COLOR`, `POSTCTL_ACCESSIBLE`, `POSTCTL_ASCII`).

## Deliverables

1. **Components** (`internal/ui`): header (product mark, workspace name, environment badge with production warning per Q10), footer key hints (context-aware), method badge, status line (glyph + text), response-status chip (for WS6), key-value table (enable/disable marker, focused row), tabs bar, modal (danger variant defaults to Cancel), empty state, error state (what happened → what's intact → next action), toast/transient status, spinner-with-text. Snapshot-style tests of rendered output at 80 and 140 columns, including `NO_COLOR` and ASCII.
2. **Root model and layout** (`internal/tui`): three layouts from `ui.LayoutFor(width)`: three panes (Sidebar | Request | Response), two panes, single pane with overlays. Focus cycling (`Tab`/`Shift-Tab`, plus direct jumps like `Alt-1/2/3` or `1/2/3` when no text field is focused), resize reflow, and pane slots with a small interface (`Pane` with `Update/View/SetSize/Focus/Blur/KeyHints`) so WS6 can drop in the request editor and response viewer. Put placeholder panes for Request and Response that show the selected request's method, URL and summary.
3. **Collection tree:** fold/unfold, method badges, fuzzy filter `/`, selection preserved across reloads, `n` new request/folder, `r` rename, `D` duplicate, `m` move, `d` delete (danger modal). Edits persist through the store (`SaveCollection`) as async commands. Files with load errors (D3) appear as error rows with the file path and reason.
4. **Environment switcher** (`E`): list, current marker, production badge, "No environment" option. The selection persists in local state (`store.SaveState`). Secret values are masked (`••••`, ASCII `****`).
5. **Command palette** (`Ctrl-P`): fuzzy search over requests (path-aware: `Payments › Charges › Create`), environments and actions registered by panes (an action registry API WS6/WS7 can extend).
6. **Help** (`?`): generated from the key map, grouped by context.
7. **First-run state:** no workspace or empty workspace → one screen explaining `postctl import postman-local` / `postctl import postman <file>` and how to create a request, with the primary action (`Enter` Create a request).
8. **File watching:** reload when YAML/JS files change on disk (e.g. after `$EDITOR` or `git pull`), debounced, preserving selection. Use `github.com/fsnotify/fsnotify`.
9. **Accessible mode:** no alt screen or animation, and a linear presentation per the design doc.
10. **Performance:** cold start to first frame < 150 ms with a 1,000-request workspace (generate one in a benchmark). Views must not allocate heavily per frame; cache rendered tree rows.

Keys follow Q11 (standard/arrows, no vim). Reserve `Ctrl-Enter` / `Ctrl-S` for Send and `e` for `$EDITOR` in help text, even though WS6 implements them.

## Tests

`teatest`-style or direct `Update` tests for focus, layout switching at breakpoints, tree operations (with a fake store), filter, palette, environment switching and persistence, reload preserving selection, and first-run. Golden renders at 80×24, 120×40 and 200×60.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green. `go run ./cmd/postctl --workspace <dir>` opens a workspace (generate a demo workspace with a small `internal/tui/testdata` fixture or `store.Init` + a model built in a test helper). It must work at 80×24 and 200×60, in dark and light, with `NO_COLOR`, `--ascii` and `--accessible`. Run the design review checklist from `docs/design-language.md` and record the results in your DONE ledger entry.

## Parallel agents

WS0.1 (model/store), WS1 (importer + `import` subcommand in `cmd/postctl`; keep your `cmd` edits minimal to avoid conflicts), WS2, WS2b, WS3. Don't edit their packages.

## Final message

What shipped; the `Pane` and action-registry interfaces WS6 will implement against; key map; screenshots as text renders at 80×24 and 140×40; deviations; checklist results; test/lint results; commit hash.
