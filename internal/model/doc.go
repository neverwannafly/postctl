// Package model defines postctl's domain types: workspaces, collections,
// folders, requests, bodies, authentication, variables, environments and
// scripts.
//
// The package is pure data. It performs no I/O, has no third-party imports,
// and holds no global state (other than the ID generator). It is the contract
// between the Postman importer, the store, the HTTP engine, the script
// runtime, the TUI and the CLI, so its shape is intentionally conservative:
//
//   - Every node (collection, folder, request, environment, globals) carries
//     a stable [ID]. Imported nodes keep their Postman IDs; new nodes get a
//     UUIDv7 from [NewID].
//   - Content that Postman Collection v2.1 can express is representable
//     without loss. Fields the model does not understand are preserved in
//     the Extra maps (or Auth.Raw) so importers never have to drop data.
//   - Zero values are meaningful and safe: a header is enabled unless
//     Disabled is set, a nil *bool setting means "use the default", and a nil
//     Auth means "inherit from the parent".
//   - Struct tags define the on-disk YAML format written by package store.
//     Renaming a tag is a schema change and needs a migration (see
//     [SchemaVersion]).
//
// Secrets: variable values marked [VarSecret] and OAuth 2.0 tokens are held in
// memory in their plain form but are never serialized to YAML; the store moves
// them to a SecretStore and writes only a [SecretRef].
//
// See docs/model.md for a diagram and YAML examples.
package model

// SchemaVersion is the current on-disk schema version. The store writes it to
// postctl.yaml and to every collection, environment and globals file, and runs
// registered migrations when it reads an older file.
const SchemaVersion = 1
