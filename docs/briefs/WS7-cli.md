# WS7 — Linear CLI: `ls`, `send`, `run`, `env`

Owner model: Grok 4.7 (high, fast) · Branch: `ws/7-cli` · Worktree: `~/work/postctl-wt/ws7-cli`

## Setup (do this first)

The orchestrator's shell is unavailable, so create your own worktree from `/Users/shubham/work/postctl`: `git worktree add -b ws/7-cli ~/work/postctl-wt/ws7-cli main`. Work only there.

## Goal

The scriptable, accessible face of postctl, and a drop-in for `newman` in CI. Every command is linear (no alt screen), honors `NO_COLOR`/`--ascii`, and has `--json`.

## Read first

`docs/plan.md` §0 and §9 (commands and exit codes: 0 OK, 1 test failures, 2 request/transport errors, 3 usage/config), `docs/engine.md` (Send, Run, RunEvent, RunSummary.ExitCode, LoadDataFile), `docs/model.md`, `AGENTS.md`, `docs/design-language.md` (text hierarchy, status glyph + word, never color alone). Code: `cmd/postctl/main.go` (existing dispatch, `import` command, global flags), `cmd/postctl/wire.go` (how the TUI builds the engine; reuse the same construction), `internal/engine`, `internal/store`, `internal/httpx` (`Curl`), and `~/work/prod-console-tui/internal/cli/` for style.

## Scope (files you own)

`internal/cli/` (commands, formatting, reporters) and the dispatch lines in `cmd/postctl/main.go` for `ls`, `send`, `run`, `env`, `curl`. Keep `import` as is. Other agents (WS6c TUI runner/history, WS9 WebSocket) may add lines in `cmd/postctl/wire.go`; don't restructure it.

## Commands

- **Path addressing:** `<collection>/<folder>/.../<request>` by name (case-insensitive, slug or display name) or by ID. Report ambiguities as an error that lists the candidates.
- `postctl ls [path] [--json]`: tree listing with method and name. `--flat` prints full paths, one per line (good for `fzf`).
- `postctl send <path> [-e env] [--var k=v]... [--json] [--include] [--verbose] [--no-scripts] [--no-history]`:
  - Output: the status line (`200 OK · 184 ms · 1.2 KB`), then the body (pretty-printed when stdout is a TTY, raw otherwise), then tests (`✓`/`×`).
  - `--include` adds response headers. `--verbose` adds the sent request (secrets masked), the timeline and console output.
  - Exit codes per §9.
- `postctl run <path> [-e env] [-d data.csv|json] [-n iterations] [--delay 200ms] [--bail] [--reporter cli,json,junit] [--reporter-json-export FILE] [--reporter-junit-export FILE] [--timeout-request D] [--no-history]`:
  - The `cli` reporter streams live progress: per request, method, name, status, time and test lines; per iteration, a header. It ends with a newman-like summary table (iterations, requests, test scripts, prerequest scripts, assertions: executed/failed; total and average response time; a failures list with request path and message).
  - JUnit XML is compatible with common CI parsers: one testsuite per request, one testcase per assertion.
  - `--json` streams NDJSON events.
  - Ctrl-C cancels cleanly and prints a partial summary.
- `postctl env ls|use <name>|show <name> [--reveal]|set <name> <key> <value> [--secret]|unset <name> <key>`. `use` writes `store.State.ActiveEnvironment`. `show` masks secrets unless `--reveal` (which asks for confirmation when stdin is a TTY). `-e` on send/run defaults to the active environment.
- `postctl curl <path> [-e env] [--with-secrets]`: prints the resolved request as a curl command (masked by default).

## Tests

- **Golden tests** for each command's text and JSON output against an `httptest` server and a temp workspace (engine with `store.MemorySecrets`).
- **JUnit:** validate the XML structure.
- **Exit codes:** a table test.
- **Path resolution:** ambiguity and case cases.
- **Masking:** assert no secret value appears in any output, unless `--reveal` or `--with-secrets` is given.
- **Private data (read-only):** a test that runs `ls` on a workspace imported from `testdata/private/staging` when present. Copy it from `/Users/shubham/work/postctl/testdata/private/staging`; it's gitignored. Never print values.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green; README-style usage examples in `docs/cli.md`; ledger entries (STARTED, PROGRESS, DONE) in your worktree's `docs/ledger.md`, plus a WS7 status row.

## Final message

Commands and flags, a sample `run` output (synthetic), exit-code table, deviations from newman, test/lint results, commit hash.
