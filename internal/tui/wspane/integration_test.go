package wspane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/wsx"
)

// TestRealClientAgainstEchoServer drives the pane with the real wsx client
// (no fakes for the network): headers, variables, saved messages and masking.
func TestRealClientAgainstEchoServer(t *testing.T) {
	var (
		mu      sync.Mutex
		gotAuth string
		gotQ    string
		wire    []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth, gotQ = r.Header.Get("X-Client"), r.URL.Query().Get("channel")
		mu.Unlock()
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		for {
			typ, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			mu.Lock()
			wire = append(wire, string(data))
			mu.Unlock()
			if err := c.Write(context.Background(), typ, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	h := newHarness(t, withReal(func(h *harness) {
		h.env.Variables = append(h.env.Variables, model.Variable{Key: "host", Value: "ws" + strings.TrimPrefix(srv.URL, "http")})
		h.req.URL = model.ParseURL("{{host}}/stream?channel=btc")
	}))

	h.key("ctrl+s")
	h.waitState(stOpen)
	h.mustHave("Connected")
	mu.Lock()
	if gotAuth != "postctl" || gotQ != "btc" {
		t.Fatalf("handshake saw header %q, channel %q", gotAuth, gotQ)
	}
	mu.Unlock()

	// A saved message with a secret: the server gets the value, the log does not show it.
	h.key("down", "tab", "down", "down", "enter") // "auth"
	h.waitLog(4)                                  // connecting, open, sent, echoed
	waitFor(t, h, func() bool { mu.Lock(); defer mu.Unlock(); return len(wire) == 1 })
	mu.Lock()
	if wire[0] != `{"type":"auth","token":"s3cr3t-value"}` {
		t.Fatalf("wire = %q", wire)
	}
	mu.Unlock()
	// The sent row is masked. (The echo is the server's own data and is shown as received.)
	var sentRow string
	for _, e := range h.p.peek().log.entries {
		if e.kind == ekMessage && e.dir == wsx.DirOut {
			sentRow = e.summary()
		}
	}
	if strings.Contains(sentRow, "s3cr3t-value") || !strings.Contains(sentRow, wsx.Mask) {
		t.Fatalf("the sent row must mask the secret: %q", sentRow)
	}

	// The composer sends what was typed.
	h.key("esc", "down")
	h.typ("hello")
	h.key("ctrl+s")
	waitFor(t, h, func() bool { mu.Lock(); defer mu.Unlock(); return len(wire) == 2 })

	// Ping and disconnect through the real connection.
	h.runAction("ws.ping")
	waitFor(t, h, func() bool {
		for _, e := range h.p.peek().log.entries {
			if e.kind == ekPong {
				return true
			}
		}
		return false
	})
	h.runAction("ws.disconnect")
	h.waitState(stIdle)
	h.mustHave("Closed 1000")
	if strings.Contains(strings.Join(h.logTexts(), "\n"), "Closed 1006") {
		t.Fatal("a client-initiated close is normal")
	}
}

func TestRealClientRefusedHandshake(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"token expired"}`))
	}))
	t.Cleanup(srv.Close)
	h := newHarness(t, withReal(func(h *harness) {
		h.req.URL = model.ParseURL("ws" + strings.TrimPrefix(srv.URL, "http") + "/stream")
	}))
	h.key("ctrl+s")
	h.waitState(stIdle)
	h.waitLog(2)
	h.mustHave("Refused: 401 Unauthorized")
	h.key("down", "tab", "tab", "enter")
	h.mustHave("token expired")
}

func TestRealClientConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/x"
	srv.Close() // nothing listens any more
	h := newHarness(t, withReal(func(h *harness) { h.req.URL = model.ParseURL(url + "?token=abc123secret") }))
	h.key("ctrl+s")
	h.waitState(stIdle)
	h.mustHave("Could not connect")
	if strings.Contains(strings.Join(h.logTexts(), "\n"), "abc123secret") {
		t.Fatal("the query string leaked into the log")
	}
}

func waitFor(t *testing.T, h *harness, cond func() bool) {
	t.Helper()
	for range 400 {
		h.settle()
		if cond() {
			return
		}
	}
	t.Fatalf("condition not met\n%s", h.view())
}

// withReal makes the harness use the real Prepare and wsx client; setup edits
// the fixture before the pane sees it.
func withReal(setup func(h *harness)) option {
	return func(h *harness) {
		h.real = true
		setup(h)
	}
}
