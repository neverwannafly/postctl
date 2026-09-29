package model

// SecretRef is an opaque key into the SecretStore (macOS Keychain). YAML files
// contain refs, never secret values.
type SecretRef string

// VarType distinguishes ordinary variables from secrets.
type VarType string

// Variable types. The empty type is equivalent to [VarDefault].
const (
	// VarDefault is an ordinary variable whose value is stored in YAML.
	VarDefault VarType = "default"
	// VarSecret is a variable whose value lives in the SecretStore.
	VarSecret VarType = "secret"
)

// Variable is a named value used for {{key}} substitution in any scope
// (collection, folder, environment, globals) and for URL path variables.
type Variable struct {
	// Key is the variable name.
	Key string `yaml:"key"`
	// Value is the value. For [VarSecret] variables it is populated in
	// memory (after hydration) but never serialized.
	Value string `yaml:"value,omitempty"`
	// Type is [VarDefault] or [VarSecret]; empty means default.
	Type VarType `yaml:"type,omitempty"`
	// DataType is the source's declared data type (string, number, boolean,
	// any), preserved for lossless import. Informational.
	DataType string `yaml:"dataType,omitempty"`
	// Description is optional documentation.
	Description string `yaml:"description,omitempty"`
	// Disabled excludes the variable from resolution.
	Disabled bool `yaml:"disabled,omitempty"`
	// SecretRef locates the value in the SecretStore for secret variables.
	// The store assigns it on save when empty. Clear it when cloning a
	// variable so the copy does not share the original's secret.
	SecretRef SecretRef `yaml:"secretRef,omitempty"`
}

// IsSecret reports whether the value lives in the SecretStore.
func (v Variable) IsSecret() bool { return v.Type == VarSecret }

// Enabled reports whether the variable participates in resolution.
func (v Variable) Enabled() bool { return !v.Disabled }

// Environment is a named set of variables (Postman: environment). The active
// environment is chosen per workspace in local state, not stored here.
type Environment struct {
	// ID is the stable identifier (Postman: environment id).
	ID ID `yaml:"id"`
	// Name is the display name.
	Name string `yaml:"name"`
	// Description is free-form text.
	Description string `yaml:"description,omitempty"`
	// Production marks a production environment; the UI shows a warning
	// badge while it is active (plan §0, Q10). It never blocks sending.
	Production bool `yaml:"production,omitempty"`
	// Variables are the environment's variables in order.
	Variables []Variable `yaml:"variables,omitempty"`
	// Extra preserves unknown fields from the source.
	Extra map[string]any `yaml:"extra,omitempty"`
}

// Globals are workspace-wide variables (Postman: globals).
type Globals struct {
	// ID is the stable identifier.
	ID ID `yaml:"id"`
	// Name is the display name (Postman uses "Globals").
	Name string `yaml:"name,omitempty"`
	// Variables are the global variables in order.
	Variables []Variable `yaml:"variables,omitempty"`
	// Extra preserves unknown fields from the source.
	Extra map[string]any `yaml:"extra,omitempty"`
}

// Lookup returns the first enabled variable with the given key.
func Lookup(vars []Variable, key string) (Variable, bool) {
	for _, v := range vars {
		if v.Key == key && !v.Disabled {
			return v, true
		}
	}
	return Variable{}, false
}
