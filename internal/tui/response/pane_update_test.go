package response

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

func TestSendStateTransitions(t *testing.T) {
	id := model.NewID()
	req := &model.Request{ID: id, Method: "GET", URL: model.URL{Raw: "https://example.com"}}
	p := &Pane{
		th:   ui.NewTheme(true, false, false),
		byID: map[model.ID]*storedResult{},
		w:    80,
		h:    20,
		spin: ui.NewTheme(true, false, false).NewSpinner(),
	}
	p.ref = tui.RequestRef{Request: req}
	p.SetSize(80, 20)

	var pane tui.Pane = p
	pane, _ = pane.Update(tui.SendStartedMsg{RequestID: id})
	p = pane.(*Pane)
	if p.sendingID != id {
		t.Fatal("expected sending")
	}
	view := p.View()
	if view == "" || !contains(view, "Sending") {
		t.Fatalf("expected sending view, got %q", view)
	}

	pane, _ = pane.Update(tui.SendProgressMsg{RequestID: id, Step: tui.SendStep{Text: "Waiting for response"}})
	pane, _ = pane.Update(tui.SendResultMsg{Result: tui.SendResult{
		RequestID: id,
		Response:  &httpx.Response{StatusCode: 200, Status: "OK", Body: []byte(`{"ok":true}`), Timings: httpx.Timings{Total: time.Millisecond}},
		Warnings:  []string{"Unresolved variable"},
	}})
	p = pane.(*Pane)
	if p.sendingID != "" {
		t.Fatal("send should finish")
	}
	view = p.View()
	if !contains(view, "200") {
		t.Fatalf("expected result chip, got %q", view)
	}
	if warning, body := strings.Index(view, "! Unresolved variable"), strings.Index(view, `"ok"`); warning < 0 || body < 0 || warning > body {
		t.Fatalf("warning should be labeled above body, got %q", view)
	}

	pane, _ = pane.Update(tui.SendResultMsg{Result: tui.SendResult{
		RequestID: id,
		Err:       errors.New("connection refused"),
	}})
	p = pane.(*Pane)
	view = p.View()
	if !contains(view, "connection refused") {
		t.Fatalf("expected transport error, got %q", view)
	}
}

func TestSelectionShowsStoredResult(t *testing.T) {
	id1, id2 := model.NewID(), model.NewID()
	p := &Pane{
		th:   ui.NewTheme(true, false, false),
		byID: map[model.ID]*storedResult{},
		w:    80,
		h:    20,
	}
	p.applyResult(tui.SendResult{RequestID: id1, Response: &httpx.Response{StatusCode: 201, Status: "Created", Body: []byte("a")}})
	p.applyResult(tui.SendResult{RequestID: id2, Response: &httpx.Response{StatusCode: 404, Status: "Not Found", Body: []byte("b")}})
	p.ref = tui.RequestRef{Request: &model.Request{ID: id1}}
	if !contains(p.View(), "201") {
		t.Fatal("expected first result")
	}
	var pane2 tui.Pane = p
	pane2, _ = pane2.Update(tui.SelectionMsg{Ref: tui.RequestRef{Request: &model.Request{ID: id2}}})
	p = pane2.(*Pane)
	if !contains(p.View(), "404") {
		t.Fatal("expected second result after selection")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && (s == sub || len(s) > 0 && stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestCancelSendOnEsc(t *testing.T) {
	id := model.NewID()
	p := &Pane{
		th:        ui.NewTheme(true, false, false),
		byID:      map[model.ID]*storedResult{},
		sendingID: id,
		ref:       tui.RequestRef{Request: &model.Request{ID: id}},
		focused:   true,
	}
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	if cmd == nil {
		t.Fatal("expected cancel cmd")
	}
	msg := cmd()
	if m, ok := msg.(tui.CancelSendMsg); !ok || m.RequestID != id {
		t.Fatalf("expected CancelSendMsg, got %T", msg)
	}
}
