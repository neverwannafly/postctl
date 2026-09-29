package model

import "time"

// WSFormat is how a saved WebSocket message body is interpreted when sent.
type WSFormat string

// WebSocket message formats.
const (
	// WSText sends Body as a text frame, exactly as written.
	WSText WSFormat = "text"
	// WSJSON sends Body as a text frame after checking that it is valid JSON
	// (once variables are resolved).
	WSJSON WSFormat = "json"
	// WSBinaryBase64 decodes Body from base64 and sends a binary frame.
	WSBinaryBase64 WSFormat = "binary-base64"
)

// Valid reports whether f is a known format. The empty format means text.
func (f WSFormat) Valid() bool {
	switch f {
	case "", WSText, WSJSON, WSBinaryBase64:
		return true
	}
	return false
}

// WSMessage is a saved WebSocket message template. Body may contain
// {{variable}} references, which are resolved when the message is sent.
type WSMessage struct {
	// ID is the stable identifier. Empty only for the unsaved composer draft.
	ID ID `yaml:"id,omitempty"`
	// Name is the display name in the saved-message list.
	Name string `yaml:"name,omitempty"`
	// Body is the message content (base64 text for [WSBinaryBase64]).
	Body string `yaml:"body,omitempty"`
	// Format selects how Body is sent; empty means [WSText].
	Format WSFormat `yaml:"format,omitempty"`
}

// WebSocketSettings holds the WebSocket-specific part of a request of kind
// [KindWebSocket]. The URL, headers, query parameters and auth live in the
// ordinary request fields; TLS verification and the handshake timeout come
// from [RequestSettings] (VerifyTLS, Timeout).
type WebSocketSettings struct {
	// Messages are the saved message templates, in display order.
	Messages []WSMessage `yaml:"messages,omitempty"`
	// Draft is the composer content, kept so unsent text survives a restart.
	Draft *WSMessage `yaml:"draft,omitempty"`
	// Subprotocols are offered in Sec-WebSocket-Protocol, in preference
	// order.
	Subprotocols []string `yaml:"subprotocols,omitempty"`
	// PingInterval sends a keep-alive ping this often while connected; nil
	// or zero disables it.
	PingInterval *time.Duration `yaml:"pingInterval,omitempty"`
	// MaxMessageBytes caps the size of one received message; nil or zero
	// means the client default (16 MiB).
	MaxMessageBytes *int64 `yaml:"maxMessageBytes,omitempty"`
	// Extra preserves unknown WebSocket fields.
	Extra map[string]any `yaml:"extra,omitempty"`
}

// NewWebSocketRequest returns a WebSocket request with a fresh ID.
func NewWebSocketRequest(name, rawURL string) *Request {
	return &Request{
		ID: NewID(), Name: name, Kind: KindWebSocket, URL: ParseURL(rawURL),
		WebSocket: &WebSocketSettings{},
	}
}

// FindMessage returns the saved message with the given ID.
func (w *WebSocketSettings) FindMessage(id ID) (*WSMessage, bool) {
	if w == nil {
		return nil, false
	}
	for i := range w.Messages {
		if w.Messages[i].ID == id {
			return &w.Messages[i], true
		}
	}
	return nil, false
}

// Clone returns a deep copy of the settings. Nil-safe.
func (w *WebSocketSettings) Clone() *WebSocketSettings {
	if w == nil {
		return nil
	}
	c := *w
	c.Messages = cloneSlice(w.Messages)
	if w.Draft != nil {
		d := *w.Draft
		c.Draft = &d
	}
	c.Subprotocols = cloneSlice(w.Subprotocols)
	c.PingInterval = clonePtr(w.PingInterval)
	c.MaxMessageBytes = clonePtr(w.MaxMessageBytes)
	c.Extra = cloneAnyMap(w.Extra)
	return &c
}

// EnsureIDs assigns a fresh [NewID] to every saved message without one.
// Nil-safe.
func (w *WebSocketSettings) EnsureIDs() {
	if w == nil {
		return
	}
	for i := range w.Messages {
		if w.Messages[i].ID == "" {
			w.Messages[i].ID = NewID()
		}
	}
}

func (w *WebSocketSettings) freshen() {
	if w == nil {
		return
	}
	for i := range w.Messages {
		if w.Messages[i].ID != "" {
			w.Messages[i].ID = NewID()
		}
	}
	if w.Draft != nil && w.Draft.ID != "" {
		w.Draft.ID = NewID()
	}
}
