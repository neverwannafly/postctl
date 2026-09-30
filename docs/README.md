# postctl documentation

## Users

- [README](../README.md) — install, quick start and common commands.
- [CLI reference](cli.md) — flags, output, reporters and exit codes.
- [Keyboard reference](keys.md) — generated shortcuts for every screen.
- [Scripting reference](scripting.md) — supported Postman `pm.*` behavior.
- [Migrate from Postman](migrating-from-postman.md) — imports and compatibility.
- [Data model](model.md) — workspace files and domain concepts.

## Contributors

Start with [CONTRIBUTING](../CONTRIBUTING.md), then spend an hour with
[onboarding](onboarding.md). [Architecture](architecture.md) explains package
boundaries, contracts, wiring, concurrency, errors, and state.

End-to-end walkthroughs:

- [Startup](flows/startup.md)
- [Send a request](flows/send-request.md)
- [Run scripts](flows/scripts.md)
- [Run a collection](flows/collection-run.md)
- [Import](flows/import.md)
- [Storage and reload](flows/storage.md)
- [TUI](flows/tui.md)
- [Authentication and secrets](flows/auth-and-secrets.md)

Binding and deep references:

- [Plan](plan.md) — §0 decisions override other design text.
- [Engine](engine.md) — detailed send/runner contract.
- [Domain model](model.md) — schema and storage contract.
- [TUI stack](tui-stack.md) and [design language](design-language.md).
- [Postman local format](postman-local-format.md).
- [Ledger](ledger.md) — append-only implementation history.

## Agents

- [AGENTS.md](../AGENTS.md) — repository-wide operating rules: map, commands, definition of done, invariants, security, testing, pitfalls, where to add things.
- [CLAUDE.md](../CLAUDE.md) — Claude Code entry point (imports `AGENTS.md`).
- Package guides, read before editing there: [TUI](../internal/tui/AGENTS.md), [script sandbox](../internal/script/AGENTS.md), [store](../internal/store/AGENTS.md), [engine](../internal/engine/AGENTS.md), [importer](../internal/importer/AGENTS.md).
- `.cursor/rules/*.mdc` — Cursor rules: an always-on core rule plus scoped rules for the TUI, scripts, tests and the importer. Each points back to `AGENTS.md`.
- `task agents:check` runs the definition-of-done gate; `task keys` regenerates [keys.md](keys.md); `task tui:smoke` drives the built TUI through a pty offline.
- [Workstream briefs](briefs/) — historical ownership and acceptance criteria.

Agents follow the same contributor path and must verify paths, symbols,
commands, privacy boundaries and generated files rather than relying only on
existing prose. `internal/qa` tests keep the agent guides' paths, links, test
names, environment variables and task names in sync with the repository.
