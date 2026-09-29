package model

import (
	"strings"
	"testing"
	"time"
)

func TestWSFormatValid(t *testing.T) {
	for _, f := range []WSFormat{"", WSText, WSJSON, WSBinaryBase64} {
		if !f.Valid() {
			t.Errorf("%q should be valid", f)
		}
	}
	if WSFormat("xml").Valid() {
		t.Error("xml must be invalid")
	}
}

func TestWebSocketValidate(t *testing.T) {
	neg := -time.Second
	negBytes := int64(-1)
	tests := []struct {
		name string
		ws   *WebSocketSettings
		want string // substring of the problem, or "" for valid
	}{
		{"nil", nil, ""},
		{"empty", &WebSocketSettings{}, ""},
		{"messages", &WebSocketSettings{Messages: []WSMessage{{ID: "a", Format: WSJSON}, {ID: "b"}, {Name: "no id yet"}, {Name: "another"}}}, ""},
		{"bad format", &WebSocketSettings{Messages: []WSMessage{{ID: "a", Format: "xml"}}}, `unknown format "xml"`},
		{"duplicate id", &WebSocketSettings{Messages: []WSMessage{{ID: "a"}, {ID: "a"}}}, `duplicate id "a"`},
		{"bad draft format", &WebSocketSettings{Draft: &WSMessage{Format: "x"}}, "draft"},
		{"negative ping", &WebSocketSettings{PingInterval: &neg}, "pingInterval"},
		{"negative max", &WebSocketSettings{MaxMessageBytes: &negBytes}, "maxMessageBytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewWebSocketRequest("ws", "wss://x.test")
			r.WebSocket = tt.ws
			err := r.Validate()
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestNewWebSocketRequest(t *testing.T) {
	r := NewWebSocketRequest("Feed", "wss://x.test/feed?a=1")
	if r.Kind != KindWebSocket || r.ID == "" || r.WebSocket == nil || r.URL.Raw != "wss://x.test/feed?a=1" {
		t.Fatalf("unexpected request: %+v", r)
	}
	if len(r.URL.Query) != 1 || r.URL.Query[0].Key != "a" {
		t.Fatalf("query not parsed: %+v", r.URL.Query)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWebSocketEnsureIDsAndFind(t *testing.T) {
	c := NewCollection("c")
	r := NewWebSocketRequest("ws", "ws://x.test")
	r.WebSocket.Messages = []WSMessage{{Name: "a"}, {ID: "keep", Name: "b"}}
	c.Items = []Item{r}
	c.EnsureIDs()
	if r.WebSocket.Messages[0].ID == "" || r.WebSocket.Messages[1].ID != "keep" {
		t.Fatalf("ids: %+v", r.WebSocket.Messages)
	}
	if m, ok := r.WebSocket.FindMessage("keep"); !ok || m.Name != "b" {
		t.Fatalf("FindMessage: %v %v", m, ok)
	}
	if _, ok := r.WebSocket.FindMessage("nope"); ok {
		t.Fatal("found a missing message")
	}
	var nilWS *WebSocketSettings
	if _, ok := nilWS.FindMessage("x"); ok {
		t.Fatal("nil settings must find nothing")
	}
	nilWS.EnsureIDs()
}
