package model

// BodyMode selects which Body field is sent.
type BodyMode string

// Body modes. The empty mode is equivalent to [BodyNone].
const (
	// BodyNone sends no body.
	BodyNone BodyMode = "none"
	// BodyRaw sends Body.Raw as-is (JSON, text, XML, ...).
	BodyRaw BodyMode = "raw"
	// BodyURLEncoded sends Body.URLEncoded as application/x-www-form-urlencoded.
	BodyURLEncoded BodyMode = "urlencoded"
	// BodyFormData sends Body.FormData as multipart/form-data.
	BodyFormData BodyMode = "formdata"
	// BodyBinary sends the file at Body.File.
	BodyBinary BodyMode = "binary"
	// BodyGraphQL sends Body.GraphQL as a JSON GraphQL request.
	BodyGraphQL BodyMode = "graphql"
)

// Body is a request payload. Mode selects the active representation; the
// other fields are retained (as Postman does) so switching modes in the editor
// does not lose typed content.
type Body struct {
	// Mode selects the active representation. Empty means none.
	Mode BodyMode `yaml:"mode,omitempty"`
	// Raw is the payload for [BodyRaw].
	Raw string `yaml:"raw,omitempty"`
	// Language is the syntax hint for Raw: json, text, javascript, xml or
	// html (Postman: options.raw.language).
	Language string `yaml:"language,omitempty"`
	// URLEncoded are the fields for [BodyURLEncoded].
	URLEncoded []KV `yaml:"urlencoded,omitempty"`
	// FormData are the parts for [BodyFormData].
	FormData []FormPart `yaml:"formData,omitempty"`
	// File is the path of the file sent for [BodyBinary]. Relative paths are
	// resolved against the workspace root.
	File string `yaml:"file,omitempty"`
	// GraphQL is the payload for [BodyGraphQL].
	GraphQL *GraphQLBody `yaml:"graphql,omitempty"`
	// Disabled keeps the body in the file but excludes it from the send
	// (Postman: body.disabled).
	Disabled bool `yaml:"disabled,omitempty"`
	// Extra preserves unknown body fields.
	Extra map[string]any `yaml:"extra,omitempty"`
}

// GraphQLBody is a GraphQL query with optional JSON variables.
type GraphQLBody struct {
	// Query is the GraphQL document.
	Query string `yaml:"query,omitempty"`
	// Variables is the JSON object text of the variables.
	Variables string `yaml:"variables,omitempty"`
}

// FormPartType is the kind of a multipart part.
type FormPartType string

// Form part types.
const (
	// FormText is a text part; Value holds the content.
	FormText FormPartType = "text"
	// FormFile is a file part; Src holds the path.
	FormFile FormPartType = "file"
)

// FormPart is one multipart/form-data part.
type FormPart struct {
	// Key is the field name.
	Key string `yaml:"key"`
	// Type is [FormText] or [FormFile]; empty means text.
	Type FormPartType `yaml:"type,omitempty"`
	// Value is the content of a text part.
	Value string `yaml:"value,omitempty"`
	// Src is the file path of a file part.
	Src string `yaml:"src,omitempty"`
	// ContentType overrides the part's Content-Type.
	ContentType string `yaml:"contentType,omitempty"`
	// Description is optional documentation for the part.
	Description string `yaml:"description,omitempty"`
	// Disabled excludes the part from the send.
	Disabled bool `yaml:"disabled,omitempty"`
}
