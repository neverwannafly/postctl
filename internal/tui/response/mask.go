package response

import (
	"strings"

	"github.com/shubham/postctl/internal/history"
)

var headerRedactor = history.NewRedactor(nil)

// SecretHeader reports whether a header name should be masked in the timeline.
func SecretHeader(name string) bool {
	return headerRedactor.Sensitive(name)
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
