package request

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	hist "github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/tuitest"
	"github.com/shubham/postctl/internal/ui"
)

func TestApplyHistoryTemplateKeepsMaskedFields(t *testing.T) {
	ws := tuitest.Demo()
	coll := ws.Collections[0]
	req := coll.Requests()[0]
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}, Deps{})
	p.SetSize(120, 40)
	_, _ = p.Update(tui.WorkspaceReloadedMsg{Workspace: ws})
	_, _ = p.Update(tui.SelectionMsg{Ref: tui.RequestRef{Collection: coll, Request: req, Path: []string{coll.Name, req.Name}}})
	preMethod := req.Method
	tmpl := hist.RequestTemplate{
		Method: "PATCH",
		URL:    "{{baseUrl}}/new",
		Headers: []hist.TemplateKV{
			{Key: "Authorization", Value: hist.Mask, Masked: true},
		},
		BodyMode:      model.BodyRaw,
		BodyRaw:       hist.Mask,
		BodyRawMasked: true,
	}
	_, cmd := p.Update(tui.ApplyRequestPatchMsg{RequestID: req.ID, Template: tmpl})
	if cmd == nil {
		t.Fatal("expected autosave cmd")
	}
	if req.Method != "PATCH" {
		t.Fatalf("method = %q", req.Method)
	}
	if !strings.Contains(p.notice, "Authorization") || !strings.Contains(p.notice, "body") {
		t.Fatalf("notice = %q", p.notice)
	}
	_, _ = p.Update(cmd())
	// Undo should restore method from before patch.
	_, _ = p.Update(tea.KeyPressMsg{Text: "ctrl+z"})
	if req.Method != preMethod {
		t.Fatalf("undo failed: method = %q", req.Method)
	}
}

func TestRestoreAutosavePersistence(t *testing.T) {
	ws := tuitest.Demo()
	coll := ws.Collections[0]
	req := coll.Requests()[0]
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}, Deps{
		After: func(_ time.Duration, msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } },
	})
	p.SetSize(120, 40)
	_, _ = p.Update(tui.WorkspaceReloadedMsg{Workspace: ws})
	_, _ = p.Update(tui.SelectionMsg{Ref: tui.RequestRef{Collection: coll, Request: req, Path: []string{coll.Name, req.Name}}})
	tmpl := hist.RequestTemplate{Method: "TRACE", URL: req.URL.Raw}
	_, cmd := p.Update(tui.ApplyRequestPatchMsg{RequestID: req.ID, Template: tmpl})
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	_, cmd = p.Update(cmd())
	for cmd != nil {
		_, cmd = p.Update(cmd())
	}
	if req.Method != "TRACE" {
		t.Fatalf("method = %q", req.Method)
	}
}

func TestPendingPatchAppliedWhenSelectionFollowsPatch(t *testing.T) {
	ws := tuitest.Demo()
	coll := ws.Collections[0]
	req := coll.Requests()[0]
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}, Deps{})
	p.SetSize(120, 40)
	_, _ = p.Update(tui.WorkspaceReloadedMsg{Workspace: ws})
	tmpl := hist.RequestTemplate{Method: "OPTIONS", URL: req.URL.Raw}
	_, _ = p.Update(tui.ApplyRequestPatchMsg{RequestID: req.ID, Template: tmpl})
	_, cmd := p.Update(tui.SelectionMsg{Ref: tui.RequestRef{Collection: coll, Request: req, Path: []string{coll.Name, req.Name}}})
	if cmd != nil {
		_, _ = p.Update(cmd())
	}
	if req.Method != "OPTIONS" {
		t.Fatalf("method = %q", req.Method)
	}
}

func TestApplyHistoryTemplateDisabledWithoutMatchingRequest(t *testing.T) {
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}, Deps{})
	p.SetSize(80, 24)
	_, cmd := p.Update(tui.ApplyRequestPatchMsg{RequestID: "nope", Template: hist.RequestTemplate{Method: "GET"}})
	if cmd != nil {
		t.Fatal("expected no cmd without open request")
	}
}
