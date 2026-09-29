package wspane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/coder/websocket"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/tuitest"
	"github.com/shubham/postctl/internal/ui"
)

// rig runs the whole shell with the real WebSocket pane, the store backend
// and the real client, delivering command results as the runtime would.
type rig struct {
	t     *testing.T
	m     tea.Model
	inbox chan tea.Msg
	live  atomic.Bool
}

func newRig(t *testing.T, dir string, w, h int) *rig {
	t.Helper()
	be := tui.NewStoreBackend(dir, store.WithSecretStore(store.NewMemorySecrets()))
	r := &rig{t: t, inbox: make(chan tea.Msg, 1024)}
	r.live.Store(true)
	t.Cleanup(func() { r.live.Store(false) })
	r.m = tui.New(tui.Options{
		Backend: be, NoWatch: true,
		NewPaneForKind: map[model.Kind]tui.PaneFactory{model.KindWebSocket: Factory(Deps{})},
	})
	r.send(tea.WindowSizeMsg{Width: w, Height: h})
	r.run(r.m.Init())
	r.pump(300 * time.Millisecond)
	return r
}

func (r *rig) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if msg != nil && r.live.Load() {
			select {
			case r.inbox <- msg:
			default:
			}
		}
	}()
}

func (r *rig) send(msg tea.Msg) {
	switch m := msg.(type) {
	case tea.BatchMsg:
		for _, c := range m {
			r.run(c)
		}
		return
	case tea.QuitMsg:
		return
	}
	var cmd tea.Cmd
	r.m, cmd = r.m.Update(msg)
	r.run(cmd)
}

// pump delivers whatever arrives for up to d, or until it is quiet.
func (r *rig) pump(d time.Duration) {
	deadline := time.After(d)
	for {
		select {
		case m := <-r.inbox:
			r.send(m)
		case <-deadline:
			return
		case <-time.After(60 * time.Millisecond):
			return
		}
	}
}

func (r *rig) keys(ks ...string) {
	r.t.Helper()
	for _, k := range ks {
		r.send(keyMsg(k))
		r.pump(200 * time.Millisecond)
	}
}

func (r *rig) typ(s string) {
	r.t.Helper()
	for _, c := range s {
		if c == ' ' {
			r.send(keyMsg("space"))
		} else {
			r.send(keyMsg(string(c)))
		}
	}
	r.pump(100 * time.Millisecond)
}

func (r *rig) screen() string { return ansi.Strip(r.m.View().Content) }

func (r *rig) waitFor(sub string) {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(r.screen(), sub) {
		if time.Now().After(deadline) {
			r.t.Fatalf("screen never showed %q:\n%s", sub, r.screen())
		}
		r.pump(100 * time.Millisecond)
	}
}

func echoServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		_ = c.Write(r.Context(), websocket.MessageText, []byte(`{"type":"welcome","server":"echo"}`))
		for {
			typ, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err := c.Write(context.Background(), typ, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/stream"
}

func writeWorkspace(t *testing.T, url string) string {
	t.Helper()
	dir := t.TempDir()
	ws := tuitest.Demo()
	c := ws.Collections[0]
	live := model.NewWebSocketRequest("Live prices", url+"?channel=btc")
	live.Headers = []model.KV{{Key: "X-Client", Value: "postctl"}}
	live.WebSocket.Messages = []model.WSMessage{
		{Name: "subscribe", Body: `{"type":"subscribe","channel":"btc"}`, Format: model.WSJSON},
		{Name: "heartbeat", Body: "ping"},
	}
	c.Items = append(c.Items, live)
	if _, err := tuitest.Write(dir, ws); err != nil {
		t.Fatal(err)
	}
	return dir
}

// openLive moves the tree cursor to the WebSocket request and opens it.
func (r *rig) openLive() {
	r.t.Helper()
	for range 40 {
		if strings.Contains(r.screen(), "WebSocket") && strings.Contains(r.screen(), "Not connected") {
			return
		}
		r.keys("down")
		r.send(keyMsg("enter"))
		r.pump(150 * time.Millisecond)
		if strings.Contains(r.screen(), "Not connected") {
			return
		}
		r.keys("alt+1")
	}
	r.t.Fatalf("could not open the WebSocket request:\n%s", r.screen())
}

func TestShellEndToEnd(t *testing.T) {
	dir := writeWorkspace(t, echoServer(t))
	r := newRig(t, dir, 140, 40)
	r.openLive()
	r.waitFor("Live prices")
	t.Logf("idle at 140x40:\n%s", r.screen())

	r.keys("ctrl+s")
	r.waitFor("Connected")
	r.waitFor("welcome")

	r.keys("down") // into the composer
	r.typ(`{"type":"ping"}`)
	r.keys("ctrl+s")
	r.waitFor("↑ 1 sent")
	r.waitFor("↓ 2 received")
	log140 := r.screen()
	t.Logf("connected with a log at 140x40:\n%s", log140)
	if !strings.Contains(log140, `{"type":"ping"}`) {
		t.Fatal("the sent message is not in the log")
	}

	r.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	r.pump(100 * time.Millisecond)
	log80 := r.screen()
	t.Logf("connected with a log at 80x24:\n%s", log80)
	for i, l := range strings.Split(log80, "\n") {
		if ui.Width(l) > 80 {
			t.Fatalf("line %d is wider than 80: %q", i, l)
		}
	}
	if n := len(strings.Split(log80, "\n")); n != 24 {
		t.Fatalf("screen is %d lines, want 24", n)
	}

	// Leaving the pane saves the draft next to the request, and the connection stays up.
	r.keys("alt+1")
	deadline := time.Now().Add(3 * time.Second)
	for {
		s, err := store.Open(dir, store.WithSecretStore(store.NewMemorySecrets()))
		if err != nil {
			t.Fatal(err)
		}
		ws, err := s.Load()
		if err != nil {
			t.Fatal(err)
		}
		var got *model.Request
		_ = ws.Collections[0].Walk(func(it model.Item, _ model.Container, _ int) error {
			if q, ok := it.(*model.Request); ok && q.Name == "Live prices" {
				got = q
			}
			return nil
		})
		if got != nil && got.WebSocket != nil && got.WebSocket.Draft != nil && got.WebSocket.Draft.Body == `{"type":"ping"}` {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the draft was not saved: %+v", got)
		}
		r.pump(100 * time.Millisecond)
	}

	// Selecting an HTTP request shows the HTTP placeholder; coming back finds the live connection.
	r.keys("up")
	r.pump(100 * time.Millisecond)
	if strings.Contains(r.screen(), "Not a WebSocket") {
		t.Fatal("the WebSocket pane must not see HTTP requests")
	}
	r.keys("down", "enter")
	r.waitFor("Connected")
}
