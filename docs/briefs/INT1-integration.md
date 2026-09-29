# INT1 — Integration: finish WS6a merge, merge WS4, wire the real engine

Owner model: GPT-5.6 Sol (medium) · Works directly in `/Users/shubham/work/postctl` on `main` (the orchestrator's shell is unavailable, so this task does the merge work it would normally do).

## State you start from

- `main` has an **uncommitted, staged merge** of `ws/6a-request-editor` (MERGE_HEAD present). Conflicts are already resolved; `go build ./... && go test -race ./... && golangci-lint run` was green before the orchestrator's shell died.
- `ws/4-engine` (worktree `~/work/postctl-wt/ws4-engine`, commit `197fd21`) is finished and verified by its agent, but not yet merged.
- The worktree `~/work/postctl-wt/ws6a-request-editor` is finished and can be removed.
- Ledger conflict resolver: `python3 /Users/shubham/work/postctl-wt/.orchestrator/resolve_ledger.py docs/ledger.md "<row prefix>" "<full new row>"`. It keeps main's status rows plus any new branch rows, and keeps both sides of entry hunks. For `go.mod`/`go.sum` conflicts, keep both sides, then run `go mod tidy`.

## Steps (commit after each)

1. `git status` to confirm the staged WS6a merge. Append this ledger entry and commit the merge as `merge ws/6a-request-editor: request pane, env editor, cookies, send wiring`:

   ```markdown
   ### 2026-09-30 02:15 IST · WS6a · Orchestrator · PROGRESS
   - Did: Verified `ws/6a-request-editor`. It stayed in scope, and contract changes only add things (`RetrySendMsg` is declared once and handled in `app.go`). Merged into `main`. Build, race tests and lint are green. Ran a pty smoke test at 140×40 on a copy of the demo workspace: opened "Create charge", pressed `Ctrl-S`, and saw the fake sender's `200 OK` rendered by WS6b's response pane with a highlighted JSON body. With no environment selected, the URL bar showed `! unresolved: baseUrl`.
   - Decisions: Accepted WS6a deviations (`fakesend` package, `BlurCmd` hook, `textin`, `wire.go` `init()`). QA follow-up: envedit, cookies and textarea adjust scroll offsets inside `View`; move that into `Update`.
   - Next: At WS4 merge, swap `newSender()` for `tuiengine` and share one cookie jar.
   ```

   Then `git worktree remove --force ~/work/postctl-wt/ws6a-request-editor`.
2. **Verify WS4 before merging:** `git diff --name-only main...ws/4-engine` should only touch `internal/engine/`, `internal/history/`, `internal/tui/tuiengine/`, `docs/engine.md`, `docs/ledger.md` (plus go.mod/go.sum). Run its tests in its worktree. Then merge `ws/4-engine` into `main` with `--no-ff`, resolve conflicts as above, and run the full check. Update the WS4 status row to "Done, merged to main".
3. **Wire the real engine** (`cmd/postctl/wire.go`, owned by nobody now): replace `newSender()`'s fake with `tuiengine.New(engine)`, where the engine is built from the same store the TUI backend uses (Keychain secrets, `httpx.NewClient(version)`, `oauth2.NewSource(...)`, history on). Keep the fake behind `POSTCTL_FAKE_SENDER=1` for demos and tests. Share **one** cookie jar: pass `engine.Cookies()` to the cookie manager via `cookies.Factory` (see WS6a's notes in the ledger). If the workspace doesn't exist yet (first run), build the engine lazily when it's first needed.
4. **Orchestrator decisions on WS4's questions** (record them in `docs/plan.md` §0 as D7–D10 and implement them):
   - D7: add `store.SaveCollectionVariables(ctx?, collectionID, vars)` (writes only the `variables` part of `collection.yaml` plus secrets, re-reading the file so concurrent edits survive) and `store.LoadGlobals()`. Switch the engine to them.
   - D8: the cookie jar stays plaintext in `.postctl/cookies.json` (local, gitignored state, like a browser's cookie store), but the file must be created with mode `0600`. Add a test.
   - D9: add `Warnings []string` to `tui.SendResult` (additive). The tuiengine adapter fills it instead of injecting console lines. The response pane shows warnings above the body as `! <text>` lines in warning color (not color alone).
   - D10: add a process-local read cache to the secret store path (wrap `SecretStore` with a caching decorator that `store.Open` uses by default; invalidate on Set/Delete). Test that N sends read the Keychain once per key.
5. **Smoke tests:**
   - Build `go build -o /tmp/postctl ./cmd/postctl`.
   - In a temp dir, create a workspace (`store.Init` via a small Go test helper, or `postctl import postman` of a synthetic fixture from `internal/importer/postman/testdata`) pointing `baseUrl` at a local `httptest`-style server. A tiny `python3 -m http.server` is fine for GET.
   - Send through the real engine in a pty at 140×40, and confirm the response pane shows the real status. Reuse `/tmp/pty_run.py` (it execs `/tmp/postctl-ws5`, so copy your binary there or adapt the script) and `/tmp/render.py` via `/tmp/venv/bin/python`.
   - Record the result in the ledger.
6. Final full check: `go build ./... && go test -race ./... && golangci-lint run`. Commit, then append an `INT1 · DONE` ledger entry.

## Rules

- Never print secret values. Don't touch `testdata/private` contents.
- Keep commits small and messages descriptive. Don't push.
- If a step is blocked, stop and report exactly what state `main` is in (git status, last commit).

## Final message

Commits made, results of each step, smoke-test outcome, anything left unresolved, and the final `git log --oneline -8`.
