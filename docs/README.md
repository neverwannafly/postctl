# postctl Documentation Index

This directory contains technical specifications, architecture decisions, user guides, and reference material for `postctl`.

## Binding References for Contributors

The following documents establish binding contracts for design, domain models, dependencies, architecture, and engineering process. All pull requests and code modifications must conform to them:

- **[`plan.md`](plan.md)**: Architectural blueprint, scope, goals, and non-goals. **Section §0 (Decisions locked) is strictly binding** over all other design decisions.
- **[`model.md`](model.md)**: The domain model schema and on-disk YAML/JS file layout (`internal/model`, `internal/store`). Struct tags and file formats are binding contracts.
- **[`design-language.md`](design-language.md)**: Clear Signal design system tokens, responsive layout rules, semantic color mappings, and accessibility constraints. Binding for all UI and visual work.
- **[`tui-stack.md`](tui-stack.md)**: Technology stack, approved dependencies, Charm v2 library pinning, and Go stdlib-first architecture boundaries.
- **[`scripting.md`](scripting.md)**: The behavioral contract for the ECMAScript `pm.*` sandbox, Chai assertions, and bundled npm packages.
- **[`ledger.md`](ledger.md)**: Append-only ledger recording workstream status, progress milestones, and design deviations. Binding work tracking protocol.
- **[`briefs/`](briefs/)**: Workstream briefs defining file ownership boundaries, milestones, and acceptance criteria for contributors.

## Guides and Reference Manuals

- **[`keys.md`](keys.md)**: Complete keyboard shortcut reference across all contexts (global, tree, request pane, response viewer, environment editor, cookies, runner, history). Generated and verified against the codebase.
- **[`cli.md`](cli.md)**: Reference manual for linear commands (`ls`, `send`, `run`, `env`, `curl`), reporter configurations (CLI, NDJSON, JUnit), and process exit codes.
- **[`migrating-from-postman.md`](migrating-from-postman.md)**: Migration guide from Postman and Newman, concept mappings, automated secrets extraction to Keychain (D2), and feature compatibility.
- **[`engine.md`](engine.md)**: Internal execution pipeline sequence, script step progression, variable persistence semantics, and runner lifecycle.
- **[`postman-local-format.md`](postman-local-format.md)**: Technical breakdown of Chromium IndexedDB LevelDB extraction from Postman Desktop on macOS.
