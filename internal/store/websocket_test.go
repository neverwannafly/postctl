package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

// wsRequestPath finds the saved YAML file of the WebSocket request.
func wsRequestPath(t *testing.T, s *Store, c *model.Collection, id model.ID) string {
	t.Helper()
	p, err := s.PathOf(c, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWebSocketRoundTrip(t *testing.T) {
	s, _, _ := newTestStore(t)
	c := model.NewCollection("Sockets")
	r := model.NewWebSocketRequest("Feed", "wss://x.test/feed")
	r.WebSocket.Messages = []model.WSMessage{{ID: "m1", Name: "Hi", Body: "hello", Format: model.WSText}}
	r.WebSocket.Subprotocols = []string{"a", "b"}
	c.Items = []model.Item{r}
	ws := &model.Workspace{ID: "w", Name: "test", Collections: []*model.Collection{c}}
	if err := s.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	loaded, _ := got.Collections[0].Items[0].(*model.Request)
	if loaded == nil || loaded.Kind != model.KindWebSocket {
		t.Fatalf("loaded %#v", got.Collections[0].Items[0])
	}
	requireEqual(t, r, loaded)

	// Saving again leaves the file byte-identical.
	path := wsRequestPath(t, s, c, r.ID)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRequest(ctx, got.Collections[0], r.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("second save changed the file:\n%s\n---\n%s", before, after)
	}
}

func TestWebSocketHandWrittenAndStrict(t *testing.T) {
	s, _, _ := newTestStore(t)
	c := model.NewCollection("Sockets")
	r := model.NewWebSocketRequest("Feed", "wss://x.test/feed")
	c.Items = []model.Item{r}
	if err := s.Save(ctx, &model.Workspace{ID: "w", Name: "test", Collections: []*model.Collection{c}}); err != nil {
		t.Fatal(err)
	}
	path := wsRequestPath(t, s, c, r.ID)

	hand := "id: hand\nname: Hand\nkind: websocket\nurl:\n  raw: ws://x.test\nwebsocket:\n  messages:\n    - name: no id\n      body: hi\n"
	if err := os.WriteFile(path, []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("hand-written file: %v", err)
	}
	loaded := got.Collections[0].Items[0].(*model.Request)
	if len(loaded.WebSocket.Messages) != 1 || loaded.WebSocket.Messages[0].Body != "hi" {
		t.Fatalf("messages: %+v", loaded.WebSocket)
	}

	bad := strings.Replace(hand, "messages:", "bogus: 1\n  messages:", 1)
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); err == nil || !strings.Contains(err.Error(), filepath.Base(path)) {
		t.Fatalf("unknown websocket key must fail naming the file, got %v", err)
	}
}
