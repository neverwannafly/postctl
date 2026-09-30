# internal/store — local rules

Read the root [`AGENTS.md`](../../AGENTS.md) and [`docs/model.md`](../../docs/model.md) (the on-disk contract) first. This package is the **only writer** of workspace files and the only owner of the `SecretStore` abstraction. Files: `store.go` (open, init, load), `save.go` (all writers), `lock.go`, `files.go` and `layout.go` (YAML shapes, naming), `secrets.go` and `keychain.go`, `discover.go`, `migrate.go`, `state.go`.

## Invariants

- **One lock for every writer.** Each write method takes the workspace lock for its whole read-modify-write: a per-directory mutex plus an advisory `flock` on `.postctl/store.lock`, so several handles in one process and separate processes (TUI next to `postctl run`) never interleave. The lock is **not reentrant**: exported writers lock and call unexported helpers that do not. A new writer must take it the same way.
- **Never write back a stale snapshot.** Callers that change some variables (engine after a send, `env set`) use `UpdateEnvironmentVariables`, `UpdateCollectionVariables`, `UpdateGlobalsVariables` (re-read under the lock, apply a `mutate` callback). Whole-file savers (`SaveEnvironment`, `SaveGlobals`, `SaveCollection`, `Save`) replace what they hold; only UI editors that own the current copy may use them.
- **Atomic and deterministic.** Temp file plus rename in the same directory; unchanged content is not rewritten; output is a pure function of the model (field order, sorted keys), so save, load, save is byte-identical. Bulk saves skip fsync on purpose.
- **Format is a contract.** `model` struct tags are the YAML. Renaming or removing a tag is a schema change: bump the version, register a `Migration` (`migrate.go`), regenerate goldens. Additive, omit-empty fields need no bump. Unknown YAML keys are an error on load unless they sit in an `extra` map.
- **Secrets.** YAML holds `secretRef` only; values and OAuth token strings go to the `SecretStore` on save. `Load` and `LoadLenient` never read the Keychain; `Hydrate`, `HydrateVariables` and `HydrateAuth` do, on copies the caller owns. `Open` wraps the store in `NewCachingSecrets` (reads cached, `Set`/`Delete` invalidate). The Keychain runs `/usr/bin/security -i` with data on stdin, never argv. No plaintext fallback, ever. Cloning a variable or auth clears `SecretRef`/`Token.Ref`.
- **Lenient load is safe to save.** `LoadLenient` returns the good nodes plus `[]*FileError`; saves must never delete a file it reported (broken request, folder, collection, environment, children of a broken folder, missing script file). Only `DeleteCollection` and `DeleteEnvironment` remove broken files.
- **The store owns file names.** Slugs, `NNN-` prefixes, `order:` lists; it never touches files it does not recognize (README.md, ...). `store.Init` writes a `.gitignore` with `.postctl/`.
- Discovery order: `WithWorkspace` (flag) > `$POSTCTL_WORKSPACE` > walk up for `postctl.yaml` > global dir (`$POSTCTL_HOME`, else `$XDG_CONFIG_HOME/postctl`, else `~/.config/postctl`).

## Tests

```sh
go test -race ./internal/store                        # ~10 s
go test ./internal/store -run TestGolden -update      # rewrites testdata/golden/workspace; review every line
```

- Use `t.TempDir()` and `store.NewMemorySecrets()`. **Never** set `POSTCTL_KEYCHAIN_TESTS=1`: that test talks to the real macOS Keychain.
- Concurrency coverage: `TestConcurrentWritersAcrossHandles`, `TestUpdateVariablesKeepsRestOfFile` (`update_test.go`); engine-level: `TestConcurrentStoreWritersLoseNothing` in `internal/engine`. Add a case when you add a writer.
- The golden workspace is built from `fixture_test.go`; a new model field needs a value there so the golden exercises it. Clone tests in `internal/model` are reflection-driven and fail when a new field is not cloned deeply.
- Assert that no secret value reaches any file (walk the temp directory), as `internal/qa` does for the whole pipeline.

## Pitfalls

- Saving a workspace loaded leniently with the old delete logic destroyed files the user still needed to fix; keep the `deletable` checks in `save.go`.
- The engine once persisted a start-of-send copy of an environment over a concurrent TUI edit; never reintroduce a "save what I loaded" path for variable writes.
- Keychain reads spawn a process per secret: hydrate only what you need, and rely on the cache.
- Adding a file kind under `.postctl/` (local state) means it must stay out of git and out of `Save` pruning.
