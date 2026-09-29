# WS1b — Local Postman extractor (IndexedDB → Collection v2.1 JSON)

Owner model: GPT-5.6 Sol (medium) · Branch: `ws/1b-postman-local` · Worktree: `~/work/postctl-wt/ws1b-postman-local`

## Goal

`postctl import postman-local` must pull the user's collections, environments, and globals **including pre-request and test scripts** directly from the installed Postman desktop app, and write them as **standard Postman Collection v2.1 / environment / globals JSON files** into a staging directory. A separate importer (WS1) converts v2.1 JSON into postctl's own model; you do not depend on WS1 or on postctl's model.

## What we already know (orchestrator findings)

- Postman.app is installed. Data dir: `~/Library/Application Support/Postman/`.
- `backup-*.json` files there are empty (169 bytes each: 0 collections, 1 environment). Useful only as a schema hint.
- Real data: Chromium IndexedDB under `Partitions/<uuid>/IndexedDB/`:
  - `15b79add-…/IndexedDB/https_desktop.postman.com_0.indexeddb.leveldb` (~3 MB, `strings` shows ~636 `collectionId` occurrences).
  - `46fccc06-…/IndexedDB/file__0.indexeddb.leveldb` (~2.7 MB) + `file__0.indexeddb.blob` (~1 MB).
  - `storage/userPartitionData.json` maps partitions (`v8Partitions`, `v8PartitionsNamespaceMeta`); use it to pick the active user/partition.
- Format: LevelDB using Chromium's `idb_cmp1` comparator; IndexedDB key encoding; values are V8 structured-clone (ValueSerializer) blobs wrapped by Blink's envelope, possibly Snappy-compressed; large values in the `.blob` dir. Postman may lock the DB while running.
- This is a cache of cloud-synced workspaces and may be partial.

## Privacy rules (user-approved, binding)

- Read-only. Always copy the IndexedDB directories to a temp dir first and open the copy. Never write to Postman's directory.
- Never print, log, commit, or put in the ledger: variable values, auth params, header values, bodies, URLs with query strings, or tokens. You may print counts, object store names, key *names*, collection/folder/request *names*, and structural shapes.
- Extracted real data goes only to `testdata/private/` (gitignored) or a user-chosen `--out` dir.
- Committed test fixtures must be synthetic (build a small LevelDB fixture in tests or commit tiny synthetic samples you create).

## Plan

### Phase A — spike (time-box it; report findings in the ledger before building)

1. Enumerate databases and object stores in the copied LevelDB(s). Identify which stores hold collections, folders, requests, responses/examples, environments, globals, and how scripts/events are represented.
2. Decide the decoding stack in Go (pure Go, `CGO_ENABLED=0`):
   - LevelDB: `github.com/syndtr/goleveldb` opened read-only with a custom comparator whose `Name()` returns `idb_cmp1` (implement Chromium IndexedDB key ordering well enough to open, or iterate raw via table/journal readers if opening is impractical).
   - V8 deserialization: implement a minimal Go decoder for the V8 ValueSerializer tags actually present (objects, arrays, strings one-/two-byte, numbers, booleans, null/undefined, dates, maps/sets if present). Reference: Chromium `v8/src/objects/value-serializer.cc`, Blink `SerializedScriptValue`, and the Python project `ccl_chromium_reader` (for format understanding only; do not add a Python runtime dependency to the product).
   - Snappy: `github.com/golang/snappy` if values are compressed.
3. Write `docs/postman-local-format.md`: object stores found, record shapes (key names only), how to reassemble a collection tree (collection → folder → request ordering), how scripts are stored, how environments/globals are stored, known gaps. If Phase A proves infeasible, stop and report with evidence and a recommended fallback (e.g. a small Node/Python helper script, or guiding the user to "Export data").

### Phase B — build

- Package `internal/importer/postmanlocal`:
  - `Locate() (Install, error)`: find data dir, partitions, the active user partition.
  - `Extract(ctx, Install, Options) (*Result, error)`: returns in-memory v2.1 collections, environments, globals plus a `Coverage` report (per collection: folders, requests, requests with pre/test scripts, examples, items missing bodies or unresolved references).
  - `WriteStaging(dir)`: writes `collections/<name>.postman_collection.json`, `environments/<name>.postman_environment.json`, `globals.postman_globals.json` using the official v2.1 schema (`https://schema.getpostman.com/json/collection/v2.1.0/collection.json`). Preserve `event[].script.exec` lines exactly, `listen: prerequest|test`, collection/folder-level events and variables, auth blocks, `_postman_id`, item order.
- CLI glue: `cmd/postctl` may not exist yet in your branch; create a small `cmd/postman-extract` dev binary (`--out DIR`, `--list`, `--dry-run`, `--partition UUID`) that prints the coverage report. WS7/orchestrator will wire it into `postctl import postman-local` later.
- Validate outputs: every emitted collection must parse as v2.1 JSON and pass a JSON-schema check (vendored schema in `internal/importer/postmanlocal/testdata/`) or at least a structural validator.
- Tests: decoder unit tests (V8 tags, key decoding) on synthetic bytes; an end-to-end test that runs against `testdata/private/` only when present (`t.Skip` otherwise).

## Acceptance

- Running the dev binary on this machine lists the user's real collections with counts, and writes staging JSON to `testdata/private/staging/`, where the number of requests with scripts is reported.
- `go test -race ./internal/importer/postmanlocal/...` green; no cgo.
- `docs/postman-local-format.md` written; ledger entries for STARTED, spike findings, DONE.
- Nothing sensitive in commits, ledger, or your final message.

## Process

- Work only in your worktree/branch. `go.mod` already exists (`module github.com/shubham/postctl`, `go 1.26`); add deps as needed. Another agent (WS0) is building `internal/model`, `internal/store`, and `cmd/postctl` in parallel; do not touch those.
- Final message to the orchestrator: feasibility verdict, decoding approach, coverage numbers (counts only), known gaps, public API of the package, test results, commit hash.
