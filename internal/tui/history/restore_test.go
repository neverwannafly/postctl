package history

import (
	"fmt"
	"strings"
	"testing"

	hist "github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/tuitest"
	"github.com/shubham/postctl/internal/ui"
)

func TestRestoreDisabledWithoutTemplate(t *testing.T) {
	m := New(tui.ScreenEnv{PaneEnv: tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}}, Deps{})
	m.detail = &hist.Entry{Request: hist.RequestSnapshot{Method: "GET", URL: "https://example.test/x"}}
	_, _ = m.beginRestore()
	if !strings.Contains(m.notice, "no request template") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestRestoreDisabledForWebSocket(t *testing.T) {
	m := New(tui.ScreenEnv{PaneEnv: tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}}, Deps{})
	m.detail = &hist.Entry{
		Template: &hist.RequestTemplate{Kind: model.KindWebSocket, Method: "GET", URL: "wss://example.test/ws"},
	}
	_, _ = m.beginRestore()
	if !strings.Contains(m.notice, "WebSocket") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestRunRestoreUsesSequenceNotBatch(t *testing.T) {
	ws := tuitest.Demo()
	m := New(tui.ScreenEnv{
		PaneEnv: tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}, Workspace: ws,
	}, Deps{})
	m.detail = &hist.Entry{
		RequestID: ws.Collections[0].Requests()[0].ID,
		Template:  &hist.RequestTemplate{Method: "GET", URL: "{{baseUrl}}/x"},
	}
	m.phase = phConfirmRestore
	m.conf.Focus = 1
	_, cmd := m.runRestore()
	if cmd == nil {
		t.Fatal("expected restore cmd")
	}
	if !strings.Contains(strings.ToLower(fmt.Sprintf("%T", cmd())), "sequence") {
		t.Fatalf("expected tea.Sequence, got %T", cmd())
	}
}

func TestTemplateURLForNewNeverUsesResolvedSnapshot(t *testing.T) {
	e := &hist.Entry{
		Request:  hist.RequestSnapshot{URL: "https://resolved.example.test/path"},
		Template: &hist.RequestTemplate{},
	}
	if got := tmplURLForNew(e); got != "" {
		t.Fatal("empty template URL must create an empty URL")
	}
}
