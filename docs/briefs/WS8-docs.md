# WS8 — Docs, sample workspace, onboarding

Owner model: Gemini 3.8 Flash (high) · Branch: `ws/8-docs` · Worktree: `~/work/postctl-wt/ws8-docs`

## Setup (do this first)

`git -C /Users/shubham/work/postctl worktree add -b ws/8-docs /Users/shubham/work/postctl-wt/ws8-docs main`. Work only there.

## Goal

Someone who has never seen postctl can install it, import their Postman data and send a request in under five minutes, and find every key and command without reading code. Distribution is local builds only (plan Q12): no GoReleaser, Homebrew or release docs.

## Read first

`docs/plan.md` (§0 in full), `AGENTS.md`, `docs/design-language.md`, `docs/tui-stack.md`, `docs/model.md`, `docs/engine.md`, `docs/scripting.md`, `docs/cli.md`, `docs/postman-local-format.md`, and the ledger DONE entries for every workstream (they list keys and deviations). Check real behavior against code: `cmd/postctl`, `internal/tui/keys.go` and the help generation, and each pane's key map.

## Deliverables

1. **`README.md`** (new, top level), modeled on `~/work/prod-console-tui/README.md` in tone. Sections:
   - What it is (one paragraph).
   - Install from source (`go install ./cmd/postctl`, PATH note, requires Go 1.26).
   - Quick start: `postctl import postman-local` (what it reads, that it's read-only, and the partial-cache caveat with the File → Export fallback), `postctl import postman <export.json>`, then `postctl`.
   - Where data lives: workspace discovery, `--workspace`/`POSTCTL_WORKSPACE`, `~/.config/postctl`, what's gitignored (`.postctl/`), secrets in Keychain.
   - A TUI key cheat sheet.
   - CLI summary linking `docs/cli.md`.
   - Scripts: the supported `pm.*` summary linking `docs/scripting.md`.
   - Accessibility options (`--accessible`, `--ascii`, `NO_COLOR`).
   - Troubleshooting: Keychain prompts, unresolved variables, OAuth redirect URIs, `Ctrl-Enter` not detected (use `Ctrl-S`), Ctrl-S swallowed by terminal flow control (`stty -ixon`).
   - Development: `task build test lint`, `task assets`, repo layout, and pointers to `docs/`.
2. **`docs/README.md`**: an index of all docs with one line each, plus which ones are binding for contributors.
3. **`docs/keys.md`**: the complete key reference per context (global, tree, request pane tabs, response pane, env editor, cookies, runner, history, WebSocket pane if merged by then; otherwise leave a clearly marked TODO section for it). Generate it from, or verify it against, the code's key maps, and add a test that fails when a key binding exists in code but not in `docs/keys.md` (or the reverse). Prefer generating the doc from the key map with `go generate`.
4. **`examples/httpbin/`**: a small committed sample workspace against `https://httpbin.org` that shows off the features. It has folders; GET/POST with JSON, form and multipart bodies; bearer and basic auth using `{{vars}}`; an environment with a secret var reference (value not committed; the README explains `postctl env set httpbin token <value> --secret`); a pre-request script that sets a timestamp and HMAC header with crypto-js; test scripts with chai; and a `setNextRequest` flow for the runner. Validate it with the store (a test that loads it with `store.Load` and runs `postctl ls`), and add a CLI test that runs it against a local fake httpbin (`httptest`) so CI never hits the network.
5. **`docs/migrating-from-postman.md`**: concept mapping (workspace/collection/folder/request, environments, globals, collection variables, the runner, newman → `postctl run`), what's supported and what's not (from `scripting.md` and the importer's lint), and the secrets migration (D2).

## Rules

Docs describe what the code does today; verify every command and key you document by running it (`go run ./cmd/postctl ...`) against the example workspace. Don't touch code outside `examples/`, `docs/`, `README.md` and the key-doc generator and test. Ledger entries (STARTED, DONE) and a WS8 status row in your worktree's `docs/ledger.md`.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green; every documented command verified.

## Final message

Files added, anything in the docs you found to be inconsistent with the code (list each, so the orchestrator can decide), test/lint results, commit hash.
