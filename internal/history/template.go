package history

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/shubham/postctl/internal/model"
)

// RequestTemplate is the redacted, unresolved request stored on a history
// entry so the TUI can restore method, URL, params, headers and body without
// writing resolved values or mask glyphs into the workspace.
type RequestTemplate struct {
	// Kind is the request kind (http or websocket).
	Kind model.Kind `json:"kind,omitempty"`
	// Method is the HTTP method as saved.
	Method string `json:"method"`
	// URL is the raw URL string, including {{variables}}.
	URL string `json:"url"`
	// URLMasked reports the URL field must not be written on restore.
	URLMasked bool `json:"urlMasked,omitempty"`
	// Query lists query parameters in order.
	Query []TemplateKV `json:"query,omitempty"`
	// PathVars are :name path variable definitions.
	PathVars []TemplateVariable `json:"pathVariables,omitempty"`
	// Headers are request headers in order.
	Headers []TemplateKV `json:"headers,omitempty"`
	// BodyMode is the active body mode.
	BodyMode model.BodyMode `json:"bodyMode,omitempty"`
	// BodyRaw is raw text, a GraphQL query, or a binary file path.
	BodyRaw string `json:"bodyRaw,omitempty"`
	// BodyRawMasked reports the raw body must keep the current value.
	BodyRawMasked bool `json:"bodyRawMasked,omitempty"`
	// BodyRawTruncated reports the body was cut at the history body cap.
	BodyRawTruncated bool `json:"bodyRawTruncated,omitempty"`
	// BodyRawEncoding is empty for UTF-8 text and "base64" for non-text bodies.
	BodyRawEncoding string `json:"bodyRawEncoding,omitempty"`
	// BodyURLEncoded holds urlencoded fields.
	BodyURLEncoded []TemplateKV `json:"bodyUrlencoded,omitempty"`
	// BodyFormData holds multipart parts (file parts store basename only).
	BodyFormData []TemplateFormPart `json:"bodyFormData,omitempty"`
	// BodyGraphQLVariables is the GraphQL variables JSON text.
	BodyGraphQLVariables string `json:"bodyGraphQLVariables,omitempty"`
	// BodyGraphQLVariablesMasked reports variables must keep the current value.
	BodyGraphQLVariablesMasked bool `json:"bodyGraphQLVariablesMasked,omitempty"`
	// BodyGraphQLVariablesTruncated reports variables were truncated.
	BodyGraphQLVariablesTruncated bool `json:"bodyGraphQLVariablesTruncated,omitempty"`
}

// TemplateKV is one key-value row in a template with explicit masking.
type TemplateKV struct {
	// Key is the parameter or header name.
	Key string `json:"key"`
	// Value is the unresolved or redacted value.
	Value string `json:"value,omitempty"`
	// Disabled excludes the row from sends.
	Disabled bool `json:"disabled,omitempty"`
	// Masked reports the value must not be written on restore.
	Masked bool `json:"masked,omitempty"`
}

// TemplateVariable is one path-variable definition in a template.
type TemplateVariable struct {
	// Key is the variable name.
	Key string `json:"key"`
	// Value is the unresolved or redacted value.
	Value string `json:"value,omitempty"`
	// Masked reports the value must not be written on restore.
	Masked bool `json:"masked,omitempty"`
}

// TemplateFormPart is one multipart part in a template.
type TemplateFormPart struct {
	// Key is the field name.
	Key string `json:"key"`
	// Type is text or file.
	Type model.FormPartType `json:"type,omitempty"`
	// Value is the text part content (redacted).
	Value string `json:"value,omitempty"`
	// ValueMasked reports a text value must not be written on restore.
	ValueMasked bool `json:"valueMasked,omitempty"`
	// Src is the file basename for a file part (never file bytes).
	Src string `json:"src,omitempty"`
	// SrcMasked reports the file path must not be written on restore.
	SrcMasked bool `json:"srcMasked,omitempty"`
	// Disabled excludes the part from sends.
	Disabled bool `json:"disabled,omitempty"`
}

var templateVarRefs = regexp.MustCompile(`^\s*((Bearer|Basic|Token)\s+)?(\{\{[^{}]+\}\}\s*)+$`)

// TemplateFromRequest builds a redacted template from an unresolved request.
func TemplateFromRequest(req *model.Request, r *Redactor) *RequestTemplate {
	if req == nil {
		return nil
	}
	t := &RequestTemplate{
		Kind:     req.Kind,
		Method:   req.Method,
		Query:    templateQuery(req.URL.Query, r),
		PathVars: templatePathVars(req.URL.PathVars, r),
		Headers:  templateHeaders(req.Headers, r),
	}
	t.URL, t.URLMasked = templateURL(req.URL, r)
	if req.Body.Disabled {
		return t
	}
	t.BodyMode = req.Body.Mode
	switch req.Body.Mode {
	case model.BodyRaw:
		t.BodyRaw, t.BodyRawTruncated, t.BodyRawEncoding, t.BodyRawMasked = templateBodyText(
			r, ContentType(req.Headers), req.Body.Raw)
	case model.BodyURLEncoded:
		t.BodyURLEncoded = templateHeaders(req.Body.URLEncoded, r)
	case model.BodyFormData:
		t.BodyFormData = templateFormParts(req.Body.FormData, r)
	case model.BodyGraphQL:
		if g := req.Body.GraphQL; g != nil {
			t.BodyRaw, t.BodyRawTruncated, t.BodyRawEncoding, t.BodyRawMasked = templateBodyText(
				r, "application/json", g.Query)
			var masked bool
			t.BodyGraphQLVariables, t.BodyGraphQLVariablesTruncated, _, masked = templateBodyText(
				r, "application/json", g.Variables)
			t.BodyGraphQLVariablesMasked = masked
		}
	case model.BodyBinary:
		src := req.Body.File
		redacted := r.Text(src)
		t.BodyRaw = redacted
		t.BodyRawMasked = redacted != src || r.Sensitive(filepath.Base(src))
	}
	return t
}

func templateURL(u model.URL, r *Redactor) (raw string, masked bool) {
	if len(u.Query) > 0 || u.Raw == "" {
		raw = rebuildURLRaw(u, r)
		orig := u.Raw
		if orig == "" {
			orig = rebuildURLRaw(u, nil)
		}
		masked = raw != orig
		return raw, masked
	}
	raw = u.Raw
	redacted := r.URL(raw)
	return redacted, redacted != raw
}

func rebuildURLRaw(u model.URL, r *Redactor) string {
	base := u.Raw
	if idx := strings.IndexByte(base, '?'); idx >= 0 {
		base = base[:idx]
	}
	if r != nil {
		base = r.URL(base)
	}
	if len(u.Query) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteByte('?')
	for i, kv := range u.Query {
		if i > 0 {
			b.WriteByte('&')
		}
		key := kv.Key
		if r != nil {
			key = r.Text(key)
		}
		val, _ := templateFieldValue(r, kv.Key, kv.Value)
		b.WriteString(url.QueryEscape(key))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(val))
	}
	return b.String()
}

func templateQuery(rows []model.KV, r *Redactor) []TemplateKV {
	return templateHeaders(rows, r)
}

func templateHeaders(rows []model.KV, r *Redactor) []TemplateKV {
	if len(rows) == 0 {
		return nil
	}
	out := make([]TemplateKV, len(rows))
	for i, kv := range rows {
		key := kv.Key
		if r != nil {
			key = r.Text(key)
		}
		val, masked := templateFieldValue(r, kv.Key, kv.Value)
		out[i] = TemplateKV{
			Key: key, Value: val, Disabled: kv.Disabled, Masked: masked,
		}
	}
	return out
}

func templatePathVars(rows []model.Variable, r *Redactor) []TemplateVariable {
	if len(rows) == 0 {
		return nil
	}
	out := make([]TemplateVariable, len(rows))
	for i, v := range rows {
		key := v.Key
		if r != nil {
			key = r.Text(key)
		}
		val, masked := templateFieldValue(r, v.Key, v.Value)
		out[i] = TemplateVariable{Key: key, Value: val, Masked: masked}
	}
	return out
}

func templateFormParts(parts []model.FormPart, r *Redactor) []TemplateFormPart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]TemplateFormPart, len(parts))
	for i, p := range parts {
		tp := TemplateFormPart{
			Key:      r.Text(p.Key),
			Type:     p.Type,
			Disabled: p.Disabled,
		}
		if p.Type == model.FormFile {
			src := filepath.Base(p.Src)
			redacted := r.Text(src)
			tp.Src = redacted
			tp.SrcMasked = redacted != src
		} else {
			tp.Value, tp.ValueMasked = templateFieldValue(r, p.Key, p.Value)
		}
		out[i] = tp
	}
	return out
}

func templateFieldValue(r *Redactor, name, value string) (string, bool) {
	if value == "" {
		return value, false
	}
	if masked := r.Text(value); masked != value {
		return masked, true
	}
	if r != nil && r.Sensitive(name) {
		if templateVarRefs.MatchString(value) {
			return value, false
		}
		return Mask, true
	}
	return value, false
}

func templateBodyText(r *Redactor, contentType, body string) (text string, truncated bool, encoding string, masked bool) {
	if body == "" {
		return "", false, "", false
	}
	snap, truncated, enc := r.SnapshotBody(contentType, []byte(body), DefaultMaxBodyBytes)
	if enc != "" {
		return snap, truncated, enc, true
	}
	if truncated || r.Body(contentType, body) != body || r.Text(body) != body {
		masked = true
	}
	return snap, truncated, "", masked
}

// FieldMasked reports whether a template field must keep the current editor
// value on restore. Legacy templates without explicit flags fall back to glyph
// detection and refuse mask glyphs in values.
func FieldMasked(explicit bool, value string) bool {
	if explicit {
		return true
	}
	return IsMasked(value) || ContainsMaskGlyph(value)
}

// IsMasked reports whether a stored template field value is the redaction
// glyph and must not be written back on restore.
func IsMasked(value string) bool {
	v := strings.TrimSpace(value)
	return v == Mask || v == "****"
}

// ContainsMaskGlyph reports whether a value carries mask characters and must
// not be written to the workspace.
func ContainsMaskGlyph(value string) bool {
	return strings.Contains(value, Mask) || strings.Contains(value, "****")
}

// RestoreSafeValue reports whether value may be written on restore.
func RestoreSafeValue(value string) bool {
	return value != "" && !ContainsMaskGlyph(value)
}
