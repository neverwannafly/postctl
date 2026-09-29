# INT2 — Integration: merge WS6c and WS7 into `main`

Owner model: GPT-5.6 Sol (medium) · Works directly in `/Users/shubham/work/postctl` on `main` (the orchestrator's shell is still unavailable).

## State you start from

- `main` is clean at INT1's last commit, except for **uncommitted, orchestrator-written brief files** under `docs/briefs/` (`WS6c-*.md`, `WS7-*.md`, `WS9-*.md`, `INT2-*.md`). Commit those first as `docs: wave 4 briefs (WS6c, WS7, WS9, INT2)`.
- Finished branches, verified by their agents:
  - `ws/6c-runner-history` at `da8a90c` (worktree `~/work/postctl-wt/ws6c-runner-history`): the TUI runner and history screens, plus additive `engine.RunInput.NoPersist` and `OnlyRequests`.
  - `ws/7-cli` at `13862c5` (worktree `~/work/postctl-wt/ws7-cli`): `internal/cli`, and `ls`/`send`/`run`/`env`/`curl` dispatch in `cmd/postctl/main.go`, plus `docs/cli.md`.
- Still running, **do not merge**: `ws/9-websocket` (WS9).
- Ledger conflict resolver: `python3 /Users/shubham/work/postctl-wt/.orchestrator/resolve_ledger.py docs/ledger.md "<row prefix>" "<full new row>"`. For `go.mod`/`go.sum` conflicts, keep both sides, then run `go mod tidy`.

## Steps (commit after each)

1. Commit the brief files (above).
2. For each branch in the order **WS7, then WS6c**:
   - Check scope: `git diff --name-only main...<branch>`. WS7 may touch `internal/cli/`, `cmd/postctl/main.go` and `docs/cli.md`. WS6c may touch `internal/tui/runner/`, `internal/tui/history/`, small additive `internal/tui` shell files, `cmd/postctl/wire.go`, `internal/engine/run*.go` and `docs/engine.md`. Both may touch `docs/ledger.md`, `go.mod` and `go.sum`. Report anything else.
   - Merge `--no-ff`, resolve conflicts, and run `go build ./... && go test -race ./... && golangci-lint run`.
   - Set the status row to "Done, merged to main", then commit.
3. **Cross-checks after both merges** (fix small integration issues directly, noting each in the ledger):
   - WS6c exports JSON only and deferred JUnit to WS7. If `internal/cli` exposes a reusable JUnit reporter, add JUnit export to the runner summary; if not, record it as a QA follow-up.
   - The key maps don't collide: run `postctl` help (`?`) in a pty and make sure `R` (tree run), `H` (history) and `Ctrl-E` (env editor) all appear and do what they say.
   - **CLI smoke** against a local server in a temp workspace:
     - `go build -o /tmp/postctl ./cmd/postctl`.
     - Create a temp workspace by importing a synthetic fixture from `internal/importer/postman/testdata`, then point `baseUrl` at `python3 -m http.server` in a temp dir.
     - Run `postctl ls --flat`, `postctl send <path>`, `postctl run <collection> --reporter cli,junit --reporter-junit-export /tmp/postctl-junit.xml`, and check the exit codes match `docs/cli.md`.
   - **Private data (read-only, never print values):** in a temp dir, `postctl import postman-local --workspace $TMP/ws --dry-run`, then `postctl ls --flat --workspace <a temp workspace imported from testdata/private/staging with --keep-literal-secrets>`. Report counts only, then delete the temp workspace. **Don't** use the real Keychain for this: pass `--keep-literal-secrets` and delete the temp dir afterwards.
   - **TUI smoke** in a pty at 140×40 (`/tmp/pty_run.py`, which execs `/tmp/postctl-ws5`, so copy your binary there; render with `/tmp/venv/bin/python /tmp/render.py 40 140`) on a copy of `internal/tui/testdata/demo`: open the runner with `R` on a collection, start it, and confirm the summary renders.
4. Remove the finished worktrees (`ws6c-runner-history`, `ws7-cli`) with `git worktree remove --force`.
5. Final full check, then append an `INT2 · DONE` ledger entry with every result.

## Rules

Never print secret values or private data values. Don't push. If a step is blocked, stop and report git status and the last commit.

## Final message

Commits made, the result of each step and cross-check, anything left for QA, and `git log --oneline -10`.
