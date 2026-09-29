# Postman desktop local format

This document records the WS1b Phase A spike against Postman Desktop on macOS.
The extractor is read-only: it selects a partition from
`storage/userPartitionData.json`, copies that partition's complete `IndexedDB`
directory to a mode-0700 temporary directory, and opens only the copy.

## Feasibility

Extraction is feasible in pure Go. `goleveldb` can open Chromium's LevelDB when
given a comparator named `idb_cmp1`, and a sequential scan avoids key-range
assumptions. Values in the current cache decode with a focused implementation of
the IndexedDB envelope, Blink `SerializedScriptValue` envelope, Snappy wrapper,
and V8 `ValueSerializer` tags.

The desktop cache is intentionally partial. Coverage and unresolved-reference
counts are part of every extraction; a Postman “File → Export” remains the
fallback when a resource was not cached.

## Partition and databases

`v8PartitionsNamespaceMeta.users.activePartition` identifies the active
partition. The observed active partition contains:

- `file__0.indexeddb.leveldb`, including the legacy `postman-app` database.
- `https_desktop.postman.com_0.indexeddb.leveldb`, including current per-user,
  per-workspace databases.

Relevant current databases and stores:

| Database suffix/name | Store | Purpose |
| --- | --- | --- |
| `-v3-collections` | `v3-collections` | Flattened collections, folders, HTTP requests, and examples |
| `-environments` | `environments` | Environment documents |
| `postman-app` | `variable_sessions` | Legacy runtime variable sessions, including a possible `globals` model |

Legacy `postman-app` also declares `collections`, `folders`, `requests`,
`responses`, `environments`, and `globals` stores. They had no current live
records in the inspected partition. Other stores (OAuth tokens, cookies,
vault/session data, history, console data, and user data) are deliberately not
decoded.

## LevelDB and IndexedDB keys

Chromium stores the comparator name `idb_cmp1` in the manifest. An IndexedDB key
prefix packs the byte lengths of database, object-store, and index IDs into the
first byte, followed by those integer IDs in little-endian form.

- Global database-name metadata begins with `00 00 00 00 c9`; the key contains
  UTF-16BE origin and database names, and the value is the truncated
  little-endian database ID.
- Object-store metadata uses index zero and metadata tag `50`; store ID is a
  varint and metadata type zero names the store in UTF-16BE.
- Object-store records use index ID one. The remaining key is an encoded
  IndexedDB user key (null, string, date, number, array, minimum, or binary).

The product only performs an unbounded sequential scan and filters decoded
prefixes. It never seeks by a user-controlled IndexedDB key.

## Value envelopes and supported tags

Record values consist of:

1. an IndexedDB value-version varint;
2. Blink `0xff` plus wire-version varint;
3. for Blink version `0x11`, either external-blob marker `0x01` or Snappy marker
   `0x02`; for wire versions 21+, a 13-byte trailer;
4. V8 `0xff` plus serialization version and one serialized value.

The Go decoder supports null/undefined/hole, booleans, signed and unsigned
integers, doubles, BigInt, UTF-8/one-byte/two-byte strings, objects, dense and
sparse arrays, object references, dates, maps, sets, array buffers, and wrapped
primitive values. Snappy-compressed values are recursively decoded. External
blob metadata is recognized; unresolved external values contribute to coverage.

## Current record shapes

Only key names are listed here.

`v3-collections` outer record:

```text
encrypted, id, timestamp, value, version
```

Common `value` keys:

```text
$kind, id, name, parentId, collectionId, items, description, variables,
auth, scripts, isPartial, isStale, createdAt, updatedAt, meta
```

HTTP request keys additionally include:

```text
method, url, headers, queryParams, body, examples
```

Nested structural shapes:

```text
items[]:       $kind, id
scripts[]:     type, language, code
auth:          type, credentials[]
credentials[]: key, value
headers[]:     key, value, disabled, $type
body:          type, content
```

Observed `$kind` values are `collection`, `http-request`, and `http-example`.
Both roots and folders use `collection`: roots have no `parentId`; folders have
`parentId` and `collectionId`. `items[]` is the authoritative ordered child
list and references another record by ID. This preserves nested folder/request
order without sorting.

Observed scripts use `beforeRequest`; conversion maps this to
`event[].listen = "prerequest"` and splits `code` on newline boundaries into
`script.exec`. The converter also maps `afterResponse` and `test` to
`listen = "test"` when present. Events can occur on collection, folder, and
request entities.

Environment records use:

```text
id, name, values, color, owner, team, uid, relativePath, isPartial, isStale
values[]: enabled, key, type, value
```

Legacy variable-session records use:

```text
id, model, modelId, workspace, values
```

A session whose `model` is `globals` becomes a standard Postman globals file.

## v2.1 conversion

Root entities become Collection v2.1 documents with the official schema URL.
Folder and request ordering follows each parent's `items[]`. Auth credentials,
variables, descriptions, disabled headers, bodies, and scripts are copied into
their v2.1 equivalents. JSON/text/XML/JavaScript bodies become raw bodies;
form-data and URL-encoded body entries retain their arrays.

Partial or missing referenced records are omitted and counted as unresolved.
Partial examples are emitted only if the cache contains a decoded response.

## Known gaps

- The cache can omit collections, request details, examples, test scripts, or
  globals. The extractor cannot reconstruct data absent from IndexedDB.
- External blob spill values are detected but not yet resolved through blob
  metadata. None were required by current live collection/environment records.
- Rare V8 host objects, regular expressions, typed-array views, WebAssembly
  transfers, and error objects are unsupported.
- The structural validator checks required Collection v2.1 shape and schema URL;
  it is not a full JSON Schema engine.
- Deleted/historical LevelDB versions are intentionally ignored.
