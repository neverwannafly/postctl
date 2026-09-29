package response

import (
	"strings"
)

var secretHeaderNames = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"cookie":              {},
	"x-api-key":           {},
	"x-auth-token":        {},
	"x-access-token":      {},
	"api-key":             {},
	"apikey":              {},
}

// SecretHeader reports whether a header name should be masked in the timeline.
func SecretHeader(name string) bool {
	_, ok := secretHeaderNames[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// SessionCookie reports whether a cookie value should be masked until revealed.
func SessionCookie(c nameFlags) bool {
	if c.Session {
		return true
	}
	n := strings.ToLower(c.Name)
	return strings.Contains(n, "session") || strings.HasSuffix(n, "sid") ||
		strings.Contains(n, "token") || strings.Contains(n, "auth")
}

type nameFlags struct {
	Name    string
	Session bool
}
