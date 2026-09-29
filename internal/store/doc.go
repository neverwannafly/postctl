// Package store loads and saves a postctl workspace as a directory tree of
// YAML and JavaScript files.
//
// Layout (see docs/model.md for examples):
//
//	<root>/
//	  postctl.yaml                    workspace metadata, collection order
//	  globals.yaml
//	  environments/<slug>.yaml
//	  collections/<slug>/
//	    collection.yaml               metadata, auth, variables, order:
//	    pre.js  test.js               collection-level scripts
//	    <NNN>-<slug>.yaml             one request per file
//	    <NNN>-<slug>.pre.js           request pre-request script
//	    <NNN>-<slug>.test.js          request test script
//	    <folder-slug>/folder.yaml     nested folder, same shape recursively
//	  .postctl/                       local state (gitignored)
//
// Guarantees:
//
//   - Writes are atomic per file (temp file in the same directory, rename) and
//     skipped when the bytes are unchanged, so saves do not touch unmodified
//     files. Single-file saves (SaveRequest, SaveEnvironment, SaveGlobals,
//     SaveMeta) also fsync, so autosaved edits survive power loss. Bulk saves
//     (Save, SaveCollection) skip fsync because flushing thousands of files
//     on macOS takes many seconds; rename atomicity still prevents torn files
//     if the process dies.
//   - Output is deterministic: the same model always produces the same bytes,
//     which keeps git diffs minimal.
//   - Secret variable values and OAuth 2.0 tokens never reach YAML. They are
//     written to the configured [SecretStore]; files hold only a
//     [model.SecretRef]. Saving a secret without a SecretStore is an error.
//   - Ordering: a container's explicit order: list wins; entries not listed
//     follow, ordered by numeric filename prefix and then name.
//
// The store owns file names. Request files are named from position and name
// (NNN-slug.yaml) and folder directories from the folder name; renaming a node
// or reordering siblings moves files on the next save, and files it no longer
// owns are removed. Files the store does not recognise (README.md, ...) are
// left alone.
//
// Structural edits (create, rename, move, delete) are performed on the
// in-memory [model.Collection] with its tree methods and persisted with
// [Store.SaveCollection], or [Store.SaveRequest] for content-only edits of a
// single request.
package store
