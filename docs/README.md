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

- [AGENTS.md](../AGENTS.md) — repository-wide operating rules.
- [CLAUDE.md](../CLAUDE.md) — agent entry guidance.
- Nested `internal/**/AGENTS.md` files — package-local constraints.
- `.cursor/rules/` — scoped Cursor rules.
- [Workstream briefs](briefs/) — historical ownership and acceptance criteria.

Agents follow the same contributor path and must verify paths, symbols,
commands, privacy boundaries and generated files rather than relying only on
existing prose.
