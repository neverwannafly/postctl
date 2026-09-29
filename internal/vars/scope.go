// Package vars holds Postman-style variable scopes and resolves {{name}} and
// {{$dynamic}} references.
//
// The types in this file are a contract shared by the HTTP engine, the script
// runtime (pm.environment, pm.collectionVariables, pm.globals, pm.variables,
// pm.iterationData) and the engine. Extend additively.
package vars

// Scope is one variable layer. Higher values take precedence when a name is
// defined in several scopes, matching Postman.
type Scope int

// Scopes from lowest to highest precedence.
const (
	Global Scope = iota
	Collection
	Environment
	Data  // current iteration's data-file row (read-only to scripts)
	Local // pm.variables.set; lives for one send or one runner iteration
)

// Store is the live variable state for one send or one runner run.
//
// Values are kept as Go values converted from JavaScript (string, float64,
// bool, nil, []any, map[string]any) so scripts see the type they stored, as in
// Postman. They are stringified only when substituted into a request or
// persisted to disk.
type Store interface {
	// Get returns the value from the highest-precedence scope defining key.
	Get(key string) (value any, scope Scope, ok bool)
	// GetIn returns the value defined in one scope.
	GetIn(s Scope, key string) (any, bool)
	// Set defines key in scope s and marks it changed.
	Set(s Scope, key string, value any)
	// Unset removes key from scope s and marks it changed.
	Unset(s Scope, key string)
	// Clear removes every key from scope s.
	Clear(s Scope)
	// Keys lists keys defined in scope s, in insertion order.
	Keys(s Scope) []string
	// Replace substitutes {{name}} and {{$dynamic}} references in template.
	// Unknown names are left as written.
	Replace(template string) string
	// Changes reports keys set or unset per scope since the Store was built,
	// so the engine can persist Global, Collection and Environment edits.
	Changes() map[Scope][]Change
}

// Change is one mutation recorded by a Store.
type Change struct {
	Key   string
	Value any
	// Unset reports that the key was removed rather than set.
	Unset bool
}
