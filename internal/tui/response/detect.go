package response

import (
	"bytes"
	"strings"
)

// BodyKind classifies response body content for viewing.
type BodyKind int

// Body content kinds.
const (
	// BodyText is plain text.
	BodyText BodyKind = iota
	// BodyJSON is JSON (or JSON-like).
	BodyJSON
	// BodyXML is XML.
	BodyXML
	// BodyHTML is HTML.
	BodyHTML
	// BodyBinary is non-text binary content.
	BodyBinary
)

const largeBodyThreshold = 5 * 1024 * 1024

// DetectBodyKind picks a view kind from Content-Type and a sniff of body.
func DetectBodyKind(contentType string, body []byte) BodyKind {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch {
	case strings.Contains(ct, "json") || ct == "application/javascript":
		return BodyJSON
	case strings.Contains(ct, "xml") || strings.Contains(ct, "soap"):
		return BodyXML
	case strings.Contains(ct, "html"):
		return BodyHTML
	case strings.HasPrefix(ct, "text/"):
		return BodyText
	case strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "audio/") ||
		strings.HasPrefix(ct, "video/") || strings.HasPrefix(ct, "application/octet-stream"):
		return BodyBinary
	}
	return sniffBody(body)
}

func sniffBody(body []byte) BodyKind {
	trim := bytes.TrimSpace(body)
	if len(trim) == 0 {
		return BodyText
	}
	if trim[0] == '{' || trim[0] == '[' {
		return BodyJSON
	}
	if trim[0] == '<' {
		if bytes.Contains(bytes.ToLower(trim[:min(64, len(trim))]), []byte("html")) {
			return BodyHTML
		}
		return BodyXML
	}
	if bytes.IndexByte(trim, 0) >= 0 {
		return BodyBinary
	}
	return BodyText
}
