@AGENTS.md

# Claude Code notes

- Prefer the Taskfile targets over ad-hoc commands: `task agents:check` (build, race tests, lint), `task keys`, `task tui:smoke`, `task build`. Keep the pre-merge gate green before every commit and before any merge.
- Nested `CLAUDE.md` files in `internal/tui`, `internal/script`, `internal/store`, `internal/engine` and `internal/importer` import the sibling `AGENTS.md`; read it before editing there.
- Never print secret values, tokens, header values, bodies or URLs with queries in tool output, commit messages or the ledger. When a command might echo data (private-data tests, `env show`, `send --include`), use synthetic workspaces or read counts only.
- Do not run `task assets` or set `POSTCTL_KEYCHAIN_TESTS` unless the user asks.
