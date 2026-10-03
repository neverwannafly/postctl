package history

import (
	"regexp"
	"strings"

	"github.com/shubham/postctl/internal/model"
)

// RequestTemplate is the redacted, unresolved request stored on a history
// entry so the TUI can restore method, URL, params, headers and body without
// writing resolved values or mask glyphs into the workspace.
type RequestTemplate struct {
	// Method is the HTTP method as saved.
	Method string `json:"method"`
	// URL is the raw URL string, including {{variables}}.
	URL string `json:"url"`
	// Query lists query parameters in order.
	Query []model.KV `json:"query,omitempty"`
	// PathVars are :name path variable definitions.
	PathVars []model.Variable `json:"pathVariables,omitempty"`
	// Headers are request headers in order.
	Headers []model.KV `json:"headers,omitempty"`
	// BodyMode is the active body mode.
	BodyMode model.BodyMode `json:"bodyMode,omitempty"`
	// BodyRaw is the raw body text when BodyMode is raw or graphql query.
	BodyRaw string `json:"bodyRaw,omitempty"`
	// BodyURLEncoded holds urlencoded fields.
	BodyURLEncoded []model.KV `json:"bodyUrlencoded,omitempty"`
	// BodyFormData holds multipart parts (file parts store Src name only).
	BodyFormData []TemplateFormPart `json:"bodyFormData,omitempty"`
	// BodyGraphQLVariables is the GraphQL variables JSON text.
	BodyGraphQLVariables string `json:"bodyGraphQLVariables,omitempty"`
	// BodyTruncated reports the body was cut at the history body cap.
	BodyTruncated bool `json:"bodyTruncated,omitempty"`
}

// TemplateFormPart is one multipart part in a template.
type TemplateFormPart struct {
	// Key is the field name.
	Key string `json:"key"`
	// Type is text or file.
	Type model.FormPartType `json:"type,omitempty"`
	// Value is the text part content (redacted).
	Value string `json:"value,omitempty"`
	// Src is the file name or path for a file part (never file bytes).
	Src string `json:"src,omitempty"`
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
		Method:   req.Method,
		URL:      templateURL(req.URL, r),
		Query:    templateKV(req.URL.Query, r),
		PathVars: templateVars(req.URL.PathVars, r),
		Headers:  templateKV(req.Headers, r),
	}
	if req.Body.Disabled {
		return t
	}
	t.BodyMode = req.Body.Mode
	switch req.Body.Mode {
	case model.BodyRaw:
		t.BodyRaw, t.BodyTruncated = templateBodyText(r, ContentType(req.Headers), req.Body.Raw)
	case model.BodyURLEncoded:
		t.BodyURLEncoded = templateKV(req.Body.URLEncoded, r)
	case model.BodyFormData:
		t.BodyFormData = templateFormParts(req.Body.FormData, r)
	case model.BodyGraphQL:
		if g := req.Body.GraphQL; g != nil {
			t.BodyRaw, t.BodyTruncated = templateBodyText(r, "application/json", g.Query)
			t.BodyGraphQLVariables, _ = templateBodyText(r, "application/json", g.Variables)
		}
	case model.BodyBinary:
		// File path only; no bytes.
		t.BodyRaw = req.Body.File
	}
	return t
}

func templateURL(u model.URL, r *Redactor) string {
	raw := u.Raw
	if raw == "" {
		return raw
	}
	return r.URL(raw)
}

func templateKV(rows []model.KV, r *Redactor) []model.KV {
	if len(rows) == 0 {
		return nil
	}
	out := make([]model.KV, len(rows))
	for i, kv := range rows {
		kv.Description = ""
		kv.Key = r.Text(kv.Key)
		kv.Value = templateFieldValue(r, kv.Key, kv.Value)
		out[i] = kv
	}
	return out
}

func templateVars(rows []model.Variable, r *Redactor) []model.Variable {
	if len(rows) == 0 {
		return nil
	}
	out := make([]model.Variable, len(rows))
	for i, v := range rows {
		v.Description = ""
		v.Key = r.Text(v.Key)
		v.Value = templateFieldValue(r, v.Key, v.Value)
		out[i] = v
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
			tp.Src = r.Text(p.Src)
		} else {
			tp.Value = templateFieldValue(r, p.Key, p.Value)
		}
		out[i] = tp
	}
	return out
}

func templateFieldValue(r *Redactor, name, value string) string {
	if value == "" {
		return value
	}
	if masked := r.Text(value); masked != value {
		return masked
	}
	if r != nil && r.Sensitive(name) {
		if templateVarRefs.MatchString(value) {
			return value
		}
		return Mask
	}
	return value
}

func templateBodyText(r *Redactor, contentType, body string) (string, bool) {
	if body == "" {
		return "", false
	}
	text, truncated, enc := r.SnapshotBody(contentType, []byte(body), DefaultMaxBodyBytes)
	if enc != "" {
		return "(binary body)", truncated
	}
	return text, truncated
}

// IsMasked reports whether a stored template field value is the redaction
// glyph and must not be written back on restore.
func IsMasked(value string) bool {
	v := strings.TrimSpace(value)
	return v == Mask || v == "****"
}
