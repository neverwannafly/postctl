package model

import "time"

// Kind is the protocol of a request.
type Kind string

// Request kinds.
const (
	// KindHTTP is a plain HTTP(S) request. Supported in v1.
	KindHTTP Kind = "http"
	// KindWebSocket is reserved for WebSocket requests (WS9).
	KindWebSocket Kind = "websocket"
	// KindGRPC is reserved for gRPC requests (v1.1).
	KindGRPC Kind = "grpc"
)

// Request is a single API request: the leaf of a collection tree.
type Request struct {
	// ID is the stable identifier (Postman: item.id). Omitted from YAML
	// only for the OriginalRequest embedded in an [Example], which has none.
	ID ID `yaml:"id,omitempty"`
	// Name is the display name.
	Name string `yaml:"name"`
	// Description is free-form text (Markdown by convention).
	Description string `yaml:"description,omitempty"`
	// Kind is the protocol. Always set; use [KindHTTP] for HTTP.
	Kind Kind `yaml:"kind"`
	// Method is the HTTP method, upper case (GET, POST, ...). Custom
	// methods are allowed. Empty for non-HTTP kinds.
	Method string `yaml:"method,omitempty"`
	// URL is the request target.
	URL URL `yaml:"url,omitempty"`
	// Headers are the request headers in order, including disabled ones.
	Headers []KV `yaml:"headers,omitempty"`
	// Body is the request payload. The zero value means no body.
	Body Body `yaml:"body,omitempty"`
	// Auth is the request's authentication. Nil means inherit from the
	// nearest folder or collection with auth set (same as [AuthInherit]).
	Auth *Auth `yaml:"auth,omitempty"`
	// Scripts are the request-level pre-request and test scripts.
	Scripts Scripts `yaml:"scripts,omitempty"`
	// Settings are per-request transport overrides.
	Settings RequestSettings `yaml:"settings,omitempty"`
	// Examples are saved responses (Postman: response[]).
	Examples []Example `yaml:"examples,omitempty"`
	// Extra preserves unknown request-level fields (for example Postman's
	// proxy and certificate blocks).
	Extra map[string]any `yaml:"extra,omitempty"`
}

// NewRequest returns an HTTP request with a fresh ID.
func NewRequest(name, method, rawURL string) *Request {
	return &Request{ID: NewID(), Name: name, Kind: KindHTTP, Method: method, URL: ParseURL(rawURL)}
}

// NewFolder returns an empty folder with a fresh ID.
func NewFolder(name string) *Folder { return &Folder{ID: NewID(), Name: name} }

// NewCollection returns an empty collection with a fresh ID.
func NewCollection(name string) *Collection { return &Collection{ID: NewID(), Name: name} }

// KV is a key/value row used for headers, query parameters and URL-encoded
// form fields. A row is enabled unless Disabled is set.
type KV struct {
	// Key is the header/parameter name.
	Key string `yaml:"key"`
	// Value is the value; may contain {{variable}} references.
	Value string `yaml:"value,omitempty"`
	// Description is optional documentation for the row.
	Description string `yaml:"description,omitempty"`
	// Disabled excludes the row from the sent request while keeping it in
	// the editor.
	Disabled bool `yaml:"disabled,omitempty"`
}

// Enabled reports whether the row participates in the sent request.
func (k KV) Enabled() bool { return !k.Disabled }

// URL is a request URL. Raw is the source of truth for what is sent; Query
// and PathVars carry metadata the raw string cannot (disabled parameters,
// descriptions).
type URL struct {
	// Raw is the full URL as typed, including {{variables}}, the query
	// string of enabled parameters, and any #fragment.
	Raw string `yaml:"raw,omitempty"`
	// Query lists every query parameter in order. Enabled entries appear in
	// Raw; disabled entries exist only here (Postman: url.query[]).
	Query []KV `yaml:"query,omitempty"`
	// PathVars describes :name path variables (Postman: url.variable[]).
	// Value is the substitution; Key excludes the leading colon.
	PathVars []Variable `yaml:"pathVariables,omitempty"`
}

// RequestSettings are per-request transport options. Pointer fields are
// tri-state: nil means "use the workspace default".
type RequestSettings struct {
	// FollowRedirects enables following 3xx responses (default true).
	FollowRedirects *bool `yaml:"followRedirects,omitempty"`
	// MaxRedirects caps redirect hops when following redirects.
	MaxRedirects *int `yaml:"maxRedirects,omitempty"`
	// FollowOriginalMethod re-sends the original method on 301/302/303
	// instead of switching to GET.
	FollowOriginalMethod *bool `yaml:"followOriginalMethod,omitempty"`
	// FollowAuthHeader keeps the Authorization header across redirects to
	// another host.
	FollowAuthHeader *bool `yaml:"followAuthHeader,omitempty"`
	// VerifyTLS enables TLS certificate verification (default true).
	VerifyTLS *bool `yaml:"verifyTLS,omitempty"`
	// Timeout is the overall request timeout; zero/nil means the default.
	Timeout *time.Duration `yaml:"timeout,omitempty"`
	// DisableBodyPruning sends a body even for methods that normally
	// carry none (GET, HEAD).
	DisableBodyPruning *bool `yaml:"disableBodyPruning,omitempty"`
	// DisableURLEncoding sends the URL exactly as typed.
	DisableURLEncoding *bool `yaml:"disableURLEncoding,omitempty"`
	// DisableCookies bypasses the cookie jar for this request.
	DisableCookies *bool `yaml:"disableCookies,omitempty"`
	// RemoveRefererOnRedirect drops Referer on redirects.
	RemoveRefererOnRedirect *bool `yaml:"removeRefererOnRedirect,omitempty"`
	// HTTPVersion is "", "auto", "http1" or "http2".
	HTTPVersion string `yaml:"httpVersion,omitempty"`
	// Extra preserves unknown settings (Postman protocolProfileBehavior).
	Extra map[string]any `yaml:"extra,omitempty"`
}

// Example is a saved response for a request (Postman: response[]).
type Example struct {
	// ID is the stable identifier, if the source had one.
	ID ID `yaml:"id,omitempty"`
	// Name is the example's display name.
	Name string `yaml:"name"`
	// OriginalRequest is the request the response was recorded for. It has
	// no Examples of its own.
	OriginalRequest *Request `yaml:"originalRequest,omitempty"`
	// Status is the reason phrase ("Created").
	Status string `yaml:"status,omitempty"`
	// Code is the numeric status code.
	Code int `yaml:"code,omitempty"`
	// Headers are the response headers in order.
	Headers []KV `yaml:"headers,omitempty"`
	// Cookies are the cookies set by the response.
	Cookies []Cookie `yaml:"cookies,omitempty"`
	// Body is the response body as text.
	Body string `yaml:"body,omitempty"`
	// PreviewLanguage is the body language hint (json, xml, html, text).
	PreviewLanguage string `yaml:"previewLanguage,omitempty"`
	// Extra preserves unknown fields.
	Extra map[string]any `yaml:"extra,omitempty"`
}

// Cookie is a cookie attached to a saved example.
type Cookie struct {
	// Name is the cookie name.
	Name string `yaml:"name"`
	// Value is the cookie value.
	Value string `yaml:"value,omitempty"`
	// Domain is the Domain attribute.
	Domain string `yaml:"domain,omitempty"`
	// Path is the Path attribute.
	Path string `yaml:"path,omitempty"`
	// Expires is the Expires attribute as written by the source.
	Expires string `yaml:"expires,omitempty"`
	// MaxAge is the Max-Age attribute as written by the source.
	MaxAge string `yaml:"maxAge,omitempty"`
	// HostOnly marks a host-only cookie.
	HostOnly bool `yaml:"hostOnly,omitempty"`
	// HTTPOnly is the HttpOnly attribute.
	HTTPOnly bool `yaml:"httpOnly,omitempty"`
	// Secure is the Secure attribute.
	Secure bool `yaml:"secure,omitempty"`
	// Session marks a session cookie.
	Session bool `yaml:"session,omitempty"`
}
