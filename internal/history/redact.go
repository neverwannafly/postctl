package history

import (
	"encoding/base64"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/shubham/postctl/internal/model"
)

// Mask replaces every redacted value.
const Mask = "••••"

// minSubstringSecret is the shortest secret that is replaced inside larger
// text. Shorter secrets are only masked when they are a whole value (a header
// value, a query value, a form field), because replacing a two-letter secret
// everywhere would destroy the text around it.
const minSubstringSecret = 4

// Redactor removes secrets from request and response snapshots before they
// are written. It masks:
//
//   - the value of any header or query/form/JSON field whose name looks like a
//     credential (Authorization, Cookie, Set-Cookie, X-Api-Key, *token*,
//     *secret*, *password* and so on), plus names the caller registers;
//   - the password in URL user info;
//   - every occurrence of a known secret variable value.
//
// A Redactor is immutable and safe for concurrent use.
type Redactor struct {
	secrets []string
	names   map[string]bool
}

// NewRedactor returns a Redactor for the given secret variable values.
// extraNames are additional header or parameter names to treat as sensitive,
// for example the name configured for API-key auth.
func NewRedactor(secretValues []string, extraNames ...string) *Redactor {
	seen := map[string]bool{}
	var secrets []string
	for _, v := range secretValues {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		secrets = append(secrets, v)
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	names := map[string]bool{}
	for _, n := range extraNames {
		if n = normalizeName(n); n != "" {
			names[n] = true
		}
	}
	return &Redactor{secrets: secrets, names: names}
}

var sensitiveParts = []string{
	"token", "secret", "password", "passwd", "apikey", "authorization",
	"signature", "credential", "privatekey", "sessionid", "cookie",
}

func normalizeName(name string) string {
	name = strings.ToLower(name)
	return strings.NewReplacer("-", "", "_", "", " ", "", ".", "").Replace(name)
}

// Sensitive reports whether a header or parameter name looks like a
// credential.
func (r *Redactor) Sensitive(name string) bool {
	n := normalizeName(name)
	if n == "" {
		return false
	}
	if r != nil && r.names[n] {
		return true
	}
	switch n {
	case "key", "auth", "sig":
		return true
	}
	for _, part := range sensitiveParts {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}

// Text replaces known secret values inside s.
func (r *Redactor) Text(s string) string {
	if r == nil || s == "" {
		return s
	}
	for _, secret := range r.secrets {
		switch {
		case len(secret) >= minSubstringSecret:
			s = strings.ReplaceAll(s, secret, Mask)
		case s == secret:
			return Mask
		}
	}
	return s
}

// Headers returns a masked copy of headers.
func (r *Redactor) Headers(headers []model.KV) []model.KV {
	if len(headers) == 0 {
		return nil
	}
	out := make([]model.KV, len(headers))
	for i, h := range headers {
		h.Description = ""
		if r.Sensitive(h.Key) && h.Value != "" {
			h.Value = Mask
		} else {
			h.Value = r.Text(h.Value)
		}
		h.Key = r.Text(h.Key)
		out[i] = h
	}
	return out
}

// URL masks the password in user info, sensitive query values, and secret
// values anywhere in the URL. The rest of the URL is kept as written.
func (r *Redactor) URL(raw string) string {
	base, fragment, hasFragment := strings.Cut(raw, "#")
	base, query, hasQuery := strings.Cut(base, "?")
	base = maskUserInfo(base)
	base = r.Text(base)
	var b strings.Builder
	b.WriteString(base)
	if hasQuery {
		b.WriteByte('?')
		b.WriteString(r.form(query))
	}
	if hasFragment {
		b.WriteByte('#')
		b.WriteString(r.Text(fragment))
	}
	return b.String()
}

func maskUserInfo(base string) string {
	_, rest, ok := strings.Cut(base, "://")
	if !ok {
		return base
	}
	authority := rest
	if i := strings.IndexByte(authority, '/'); i >= 0 {
		authority = authority[:i]
	}
	at := strings.LastIndexByte(authority, '@')
	if at < 0 {
		return base
	}
	user, _, hasPass := strings.Cut(authority[:at], ":")
	if !hasPass {
		return base
	}
	return strings.Replace(base, authority[:at], user+":"+Mask, 1)
}

// form masks an application/x-www-form-urlencoded string or a query string.
func (r *Redactor) form(s string) string {
	if s == "" {
		return s
	}
	parts := strings.Split(s, "&")
	for i, part := range parts {
		key, value, hasValue := strings.Cut(part, "=")
		if !hasValue {
			parts[i] = r.Text(part)
			continue
		}
		name, err := url.QueryUnescape(key)
		if err != nil {
			name = key
		}
		if r.Sensitive(name) && value != "" {
			parts[i] = key + "=" + Mask
			continue
		}
		if plain, err := url.QueryUnescape(value); err == nil && r.Text(plain) != plain {
			parts[i] = key + "=" + Mask
			continue
		}
		parts[i] = key + "=" + r.Text(value)
	}
	return strings.Join(parts, "&")
}

var jsonString = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"(\s*:\s*)"((?:[^"\\]|\\.)*)"`)

// Body returns the redacted text of body. Form-encoded and JSON bodies have
// the values of credential-looking fields masked; every body has known secret
// values replaced. Binary content is not represented here (see [Snapshot]).
func (r *Redactor) Body(contentType string, body string) string {
	if body == "" {
		return body
	}
	ct := strings.ToLower(contentType)
	trimmed := strings.TrimSpace(body)
	switch {
	case strings.Contains(ct, "x-www-form-urlencoded"):
		body = r.form(body)
	case strings.Contains(ct, "json") || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "["):
		body = jsonString.ReplaceAllStringFunc(body, func(m string) string {
			sub := jsonString.FindStringSubmatch(m)
			if len(sub) != 4 || !r.Sensitive(sub[1]) || sub[3] == "" {
				return m
			}
			return `"` + sub[1] + `"` + sub[2] + `"` + Mask + `"`
		})
	}
	return r.Text(body)
}

// ContentType returns the value of the Content-Type header, if any.
func ContentType(headers []model.KV) string {
	for _, h := range headers {
		if strings.EqualFold(h.Key, "Content-Type") {
			return h.Value
		}
	}
	return ""
}

// SnapshotBody redacts body and caps it at limit bytes. It returns the text,
// whether the body was cut, and the encoding: "" for UTF-8 text, "base64" for
// content that is not valid UTF-8 (known secret values are masked in it before
// encoding).
func (r *Redactor) SnapshotBody(contentType string, body []byte, limit int) (text string, truncated bool, encoding string) {
	if limit <= 0 {
		limit = DefaultMaxBodyBytes
	}
	// Look a little past the cap so a secret straddling it is still seen.
	window := body
	if len(window) > limit+4096 {
		window = window[:limit+4096]
	}
	cut := len(window) < len(body)
	check := window
	if cut {
		check = trimIncomplete(window)
	}
	if !utf8.Valid(check) {
		raw := string(window)
		if r != nil {
			for _, secret := range r.secrets {
				if len(secret) >= minSubstringSecret {
					raw = strings.ReplaceAll(raw, secret, Mask)
				}
			}
		}
		if len(raw) > limit {
			raw = raw[:limit]
			cut = true
		}
		return base64.StdEncoding.EncodeToString([]byte(raw)), cut, "base64"
	}
	redacted := r.Body(contentType, string(check))
	if len(redacted) > limit {
		redacted = string(trimIncomplete([]byte(redacted[:limit])))
		cut = true
	}
	return redacted, cut, ""
}

// trimIncomplete drops a trailing partial UTF-8 sequence created by cutting a
// byte slice.
func trimIncomplete(b []byte) []byte {
	for i := 0; i < utf8.UTFMax && i < len(b); i++ {
		r, size := utf8.DecodeLastRune(b)
		if r != utf8.RuneError || size != 1 {
			return b
		}
		b = b[:len(b)-1]
	}
	return b
}
