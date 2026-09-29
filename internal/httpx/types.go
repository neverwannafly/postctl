// Package httpx sends fully resolved requests and reports what happened on the
// wire. It knows nothing about variables or scripts: callers resolve
// {{variables}} first (package vars) and run scripts around Send (package
// script, orchestrated by package engine).
//
// The types in this file are a contract shared by the HTTP engine, the script
// runtime (pm.request, pm.response, pm.sendRequest), the engine and the TUI.
// Extend them additively; do not rename or remove fields without updating
// every consumer.
package httpx

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/shubham/postctl/internal/model"
)

// Sender sends one request. The request must already be resolved: no
// {{variable}} references remain and inherited auth has been flattened onto
// the request (Auth is never inherit here).
type Sender interface {
	Send(ctx context.Context, req *model.Request, opts Options) (*Response, error)
}

// Options are per-send knobs that are not part of the saved request.
type Options struct {
	// WorkspaceDir resolves relative file paths in binary and form-data bodies.
	WorkspaceDir string
	// MaxBodyBytes caps how much of the response body is kept in memory;
	// zero means the package default. The rest is discarded and
	// Response.BodyTruncated is set.
	MaxBodyBytes int64
	// Defaults supplies settings for nil fields in the request's Settings.
	Defaults model.RequestSettings
}

// Response is the result of a completed exchange. A transport failure
// (DNS, refused connection, timeout, TLS) is returned as an error, not a
// Response; an HTTP error status is a normal Response.
type Response struct {
	// StatusCode is the numeric status, e.g. 201.
	StatusCode int
	// Status is the reason phrase without the code, e.g. "Created".
	Status string
	// Proto is the protocol, e.g. "HTTP/2.0".
	Proto string
	// Headers are the response headers in wire order, duplicates preserved.
	Headers []model.KV
	// Cookies are the Set-Cookie values parsed from this response.
	Cookies []model.Cookie
	// Body is the decoded (decompressed) body, possibly truncated.
	Body []byte
	// BodyTruncated reports that Body was capped by Options.MaxBodyBytes.
	BodyTruncated bool
	// Size is what crossed the wire.
	Size Size
	// Timings is the phase breakdown of the final hop.
	Timings Timings
	// Redirects lists each followed hop before the final response, in order.
	Redirects []Redirect
	// RemoteAddr is the peer address of the final connection, if known.
	RemoteAddr string
	// TLS describes the final connection's TLS state; nil for plain HTTP.
	TLS *tls.ConnectionState
	// Sent is the request exactly as it went out on the final hop, including
	// headers added by auth, cookies and defaults (User-Agent, Content-Type).
	Sent SentRequest
}

// Size counts bytes on the wire.
type Size struct {
	// Headers is the size of the response status line and headers.
	Headers int64
	// Body is the size of the response body before decompression.
	Body int64
}

// Timings are phase durations. Phases that did not happen (reused
// connection, plain HTTP) are zero.
type Timings struct {
	DNS      time.Duration
	Connect  time.Duration
	TLS      time.Duration
	TTFB     time.Duration
	Download time.Duration
	Total    time.Duration
}

// Redirect is one followed 3xx hop.
type Redirect struct {
	StatusCode int
	Location   string
}

// SentRequest is the wire view of the request that produced a Response.
type SentRequest struct {
	Method  string
	URL     string
	Headers []model.KV
	// Body is the encoded request body (multipart and urlencoded included).
	Body []byte
}
