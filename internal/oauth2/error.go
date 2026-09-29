package oauth2

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Error is a failed OAuth 2.0 token request or callback. Code and Description
// come from the provider's error and error_description fields when those are
// present. The message never includes token or authorization-code values.
type Error struct {
	// Code is the OAuth error code, such as "invalid_grant" or "access_denied".
	Code string
	// Description is the provider's error_description, when it sent one.
	Description string
	// Status is the HTTP status of a token-endpoint response. It is zero for
	// failures that did not come from that response.
	Status int
}

// Error returns a user-readable message without secret material.
func (e *Error) Error() string {
	if e == nil {
		return "oauth2: request failed"
	}
	code := cleanText(e.Code, 64)
	desc := cleanText(e.Description, 300)
	switch {
	case code != "" && desc != "":
		return "oauth2: " + code + ": " + desc
	case code != "":
		return "oauth2: " + code
	case desc != "":
		return "oauth2: " + desc
	case e.Status != 0:
		return fmt.Sprintf("oauth2: token request failed (HTTP %d)", e.Status)
	default:
		return "oauth2: request failed"
	}
}

func cleanText(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if maxRunes > 0 && utf8.RuneCountInString(s) > maxRunes {
		r := []rune(s)
		s = string(r[:maxRunes])
	}
	return s
}
