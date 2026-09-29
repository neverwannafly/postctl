package model

import "strings"

// ParseURL builds a [URL] from a raw string: Raw is kept verbatim, query
// parameters are split out of the query string (without percent-decoding, so
// they round-trip exactly), and ":name" path segments become path variables.
func ParseURL(raw string) URL {
	u := URL{Raw: raw}
	base, frag := splitFragment(raw)
	_ = frag
	path, query, hasQuery := strings.Cut(base, "?")
	if hasQuery && query != "" {
		for _, pair := range strings.Split(query, "&") {
			if pair == "" {
				continue
			}
			k, v, _ := strings.Cut(pair, "=")
			u.Query = append(u.Query, KV{Key: k, Value: v})
		}
	}
	u.PathVars = pathVars(path)
	return u
}

// pathVars finds ":name" segments in the path portion of a URL (after the
// authority), ignoring the port colon and {{variable}} references.
func pathVars(base string) []Variable {
	rest := base
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return nil
	}
	var out []Variable
	seen := map[string]bool{}
	for _, seg := range strings.Split(rest[slash+1:], "/") {
		if len(seg) > 1 && seg[0] == ':' && !seen[seg[1:]] {
			seen[seg[1:]] = true
			out = append(out, Variable{Key: seg[1:]})
		}
	}
	return out
}

func splitFragment(raw string) (base, frag string) {
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		return raw[:i], raw[i:]
	}
	return raw, ""
}

// EnabledQuery returns the query parameters that are sent.
func (u URL) EnabledQuery() []KV {
	var out []KV
	for _, q := range u.Query {
		if !q.Disabled {
			out = append(out, q)
		}
	}
	return out
}

// SyncRaw rebuilds Raw from its own scheme/host/path, the enabled entries of
// Query, and the original #fragment. Call it after editing Query so Raw stays
// consistent. Parameters with an empty Value are written as "key=".
func (u *URL) SyncRaw() {
	base, frag := splitFragment(u.Raw)
	base, _, _ = strings.Cut(base, "?")
	var b strings.Builder
	b.WriteString(base)
	sep := byte('?')
	for _, q := range u.EnabledQuery() {
		b.WriteByte(sep)
		sep = '&'
		b.WriteString(q.Key)
		b.WriteByte('=')
		b.WriteString(q.Value)
	}
	b.WriteString(frag)
	u.Raw = b.String()
}
