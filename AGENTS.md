# Agent instructions

postctl is a keyboard-first terminal replacement for Postman, written in Go on Bubble Tea v2. Several agents work on it in parallel, each owning one workstream in its own git worktree and branch.

## Required references

Read these before changing code, and keep code and documentation consistent with them:

- [`docs/plan.md`](docs/plan.md): scope and architecture. The **§0 decisions are binding**.
- Your workstream brief in `docs/briefs/`: scope, file ownership, acceptance, process.
- [`docs/model.md`](docs/model.md): the domain model and on-disk format that every workstream builds on.
- [`docs/tui-stack.md`](docs/tui-stack.md): the approved technology stack, package boundaries, and dependency policy.
- [`docs/design-language.md`](docs/design-language.md): the Clear Signal theme, semantic tokens, responsive layout, interaction, accessibility, and screen behavior. Required before any TUI or visual change.

If a requested change conflicts with one of these documents, call out the conflict and update the document as part of the change instead of silently establishing a second convention. If a plan decision seems wrong, do not deviate silently: record it under "Blockers / questions" in the ledger and pick the least surprising option.

## Process

- **Append to [`docs/ledger.md`](docs/ledger.md)** when you start, at each meaningful milestone, when blocked, and when you finish, using the format at the top of that file. Never edit or delete another agent's entry; add a correction entry instead. Keep your row in the status board current.
- **Stay inside your brief's file ownership.** Do not create or edit files owned by another workstream. If you need a change in another workstream's code (for example a new field in `internal/model`), write it as a request in the ledger for the orchestrator rather than editing it.
- Work only in your own worktree and branch. Do not touch `main`, and do not push or merge; the orchestrator merges.
- Commit early and often. Before finishing, `task build test lint` (or `go build ./... && go test -race ./... && golangci-lint run`) must pass.

## Architecture rules

- **Effects live outside views.** HTTP, scripts, disk, and Keychain work run as `tea.Cmd`s that return typed messages. `View` renders explicit state only.
- **`internal/engine` must not import TUI packages** (`internal/tui`, `internal/ui`, or any Charm module). The TUI and the CLI are both thin clients over the engine.
- `internal/model` is pure data: no I/O and no third-party imports. Its struct tags are the on-disk format; changing a tag is a schema change that needs a migration in `internal/store` and a golden-file update.
- Put storage, secrets, HTTP, and script execution behind narrow interfaces so state transitions can use fakes.
- Scripts run with a hard timeout and a cancellable context. A runaway script must never freeze the UI.
- Prefer the standard library. A new third-party dependency needs a reason recorded in `docs/tui-stack.md`, and must be pure Go (no cgo).

## UI implementation requirements

- **Semantic tokens only.** Use tokens from `internal/ui` (`text.*`, `accent.primary`, `status.*`, `method.*`, `syntax.*`, `border.subtle`, `surface.selected`). Do not add one-off colors, and never rely on color alone to convey status, selection, an HTTP method, or an error: pair it with text, a prefix, or a glyph.
- Preserve keyboard-only operation, visible key hints, resize behavior across the 100 and 140 column breakpoints, accessible/linear output, and the `NO_COLOR` / `--ascii` / `--accessible` fallbacks. The UI must stay usable at 80 by 24.
- Keep the active environment visible in the header; show the warning badge for production environments. Sending never requires confirmation.
- Standard keys and arrows (no vim bindings): `Ctrl-Enter` or `Ctrl-S` sends, `Ctrl-P` opens the palette.

## Secrets and privacy

- **No secrets in logs, ledger, tests, or chat.** Never print, log, or commit secret variable values, tokens, header values, request or response bodies, or URLs with query strings. Log counts, key names, and structure only.
- Secret values live in the Keychain via `store.SecretStore`; YAML files hold only a `secretRef`. Never add a plaintext fallback.
- Real Postman data is company data. It may only be read locally, read-only, from a temp copy, and extracted fixtures belong in `testdata/private/` (gitignored). Committed fixtures must be synthetic.

## Testing conventions

- Table-driven tests with fakes: `store.MemorySecrets` for the Keychain, `httptest` for HTTP, `t.TempDir()` for storage.
- Golden files for on-disk formats live under `testdata/golden/`; regenerate deliberately with `-update` and review the diff.
- Tests that need real data or the real Keychain are opt-in (skip when absent), for example `POSTCTL_KEYCHAIN_TESTS=1`.
- Exported identifiers need godoc comments (`golangci-lint` enforces it).
