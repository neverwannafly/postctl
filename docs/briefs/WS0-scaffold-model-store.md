# WS0 — Scaffold, domain model, workspace store

Owner model: Sonnet 5.5 (high) · Branch: `ws/0-scaffold` · Worktree: `~/work/postctl-wt/ws0-scaffold`

## Why this matters

Every other workstream builds on the types and on-disk format you define. Optimize for **clarity and stability of contracts** over feature count. Other agents will start coding against your `internal/model` and `internal/store` as soon as you merge.

## Read first

1. `docs/plan.md` (§0 decisions are binding; §4 architecture; §5 storage; §7 scripts; §8 features).
2. `~/work/prod-console-tui/docs/design-language.md` and `docs/tui-stack.md` (ignore Okta).
3. `~/work/prod-console-tui/AGENTS.md`, and skim `internal/ui/theme.go`, `internal/ui/components.go` for code style.

## Scope (you own these files)

1. **Repo scaffold**
   - `cmd/postctl/main.go`: `--version` (via `-X main.version`), `--help`, subcommand dispatch stub (`import`, `ls`, `send`, `run`, `env`) that prints "not implemented yet" with exit code 3. Use stdlib `flag` (no cobra), mirroring prod-console-tui's `internal/cli` style.
   - `Taskfile.yml`: `build`, `test` (`go test -race ./...`), `lint` (`golangci-lint run`), `fmt`.
   - `.golangci.yml` with a sensible, not noisy, linter set.
   - `AGENTS.md` for this repo: adapted from prod-console-tui's (design refs, effects outside views, semantic tokens only, no secrets in logs), plus: "append to `docs/ledger.md`", "stay inside your brief's file ownership", "`internal/engine` must not import TUI packages".
2. **Design doc port**: `docs/design-language.md` — copy the Clear Signal doc from prod-console-tui, rename the product to postctl, drop Okta/console-specific screens, and add the tokens from plan §3 (method colors, syntax tokens, status classes, production env badge) and the responsive layout breakpoints (≥140 three panes, 100–139 two, <100 single + overlays). Replace screen sketches with postctl ones (tree, request editor, response, env switcher, empty first-run state, import report). Also `docs/tui-stack.md` adapted (drop Okta, keep dependency policy, add goja/chroma/gjson rationale).
3. **`internal/model`**: pure data types, no I/O, no third-party imports except where unavoidable. Must cover everything the Postman importer needs to land losslessly:
   - `Workspace`, `Collection`, `Folder` (nested), `Request`, `Item` ordering.
   - `Request`: `Kind` (`http` now; `websocket` reserved; `grpc` reserved for v1.1), `Method`, `URL` (raw string plus parsed query params with enabled flag and descriptions, path variables), `Headers []KV` (enabled/disabled, description), `Body` (modes: none, raw with language, urlencoded, form-data with text/file parts, binary file path, graphql query+variables), `Auth`, `Scripts` (pre-request, test), `Settings` (follow redirects, timeout, TLS verify, etc.), `Examples` (saved responses), `Description`.
   - `Auth`: type enum (`inherit`, `none`, `bearer`, `basic`, `apikey`, `digest`, `oauth2`, `awsv4`, `hawk`, `ntlm`, `unsupported`) with typed params and a raw `map[string]any` fallback to preserve unknown fields.
   - `OAuth2Config` covering client credentials, auth code + PKCE, password, implicit (see plan §0 R2).
   - `Variable` (key, value, type `default|secret`, enabled, description). `SecretRef` for Keychain-backed values.
   - `Environment` (name, vars, `Production bool`), `Globals`.
   - `Script` (language `js`, source text, or file path relative to its YAML; `Exec` lines preserved).
   - Stable `ID` on every node (string; importer will use Postman IDs; new items get ULID/UUIDv7).
   - Godoc on every exported type/field. Add a `docs/model.md` with a diagram and the YAML examples.
4. **`internal/store`**: load/save a workspace directory per plan §5.
   - Workspace discovery: walk up from cwd for `postctl.yaml`; fall back to `~/.config/postctl/` (respect `XDG_CONFIG_HOME`, `POSTCTL_HOME` override).
   - Layout: `collections/<slug>/collection.yaml`, nested folders with `folder.yaml`, one `<order>-<slug>.yaml` per request, scripts in sibling `.js` files (`<request-slug>.pre.js`, `<request-slug>.test.js`; collection/folder level `pre.js`/`test.js`), `environments/<slug>.yaml`, `globals.yaml`, `.postctl/` for local state.
   - Stable ordering: explicit `order:` list in `collection.yaml`/`folder.yaml` wins; numeric prefixes as fallback.
   - Atomic writes (temp file + rename), create/rename/move/delete of items, slug collision handling, and a `schemaVersion` field with a migration hook.
   - YAML lib: `go.yaml.in/yaml/v3` (or `gopkg.in/yaml.v3`); keep output deterministic and human-friendly (block scalars for bodies).
   - Secrets: a `SecretStore` interface (`Get/Set/Delete(ref)`) with an in-memory fake and a macOS Keychain implementation (look at how prod-console-tui's `internal/credentials` does it; `/usr/bin/security` via `os/exec` without a shell, or `zalando/go-keyring`). Secret var values never written to YAML.
   - Round-trip tests: build a model in Go → save → load → deep-equal. Golden-file tests for YAML output under `internal/store/testdata/`.
5. **`internal/ui/theme.go` skeleton**: only semantic token definitions (dark/light values) and the style struct; no components (WS5 owns components). This lets WS5 start from a real token set.

## Out of scope

Postman import, HTTP, scripts, TUI screens, CLI command behavior.

## Acceptance

- `task build test lint` all pass; `go test -race ./...` green.
- `postctl --version` and `postctl --help` work.
- Round-trip and golden tests cover: nested folders, all body modes, all auth types, disabled headers/params, secret vars, scripts as files, ordering.
- `docs/model.md`, `docs/design-language.md`, `docs/tui-stack.md`, `AGENTS.md` exist and are consistent with plan §0.

## Process

- Commit early and often on `ws/0-scaffold` in your worktree. Do not touch `main`; the orchestrator merges.
- Append ledger entries to `docs/ledger.md` in **your worktree** (STARTED, a PROGRESS entry per major item, DONE). Use the format at the top of the ledger.
- If a plan decision seems wrong, do not silently deviate: write it under "Blockers / questions" and pick the least surprising option.
- Final message to the orchestrator: summary of what shipped, exported API surface of `model` and `store` (type names + key funcs), any deviations, test results, and the commit hash.
