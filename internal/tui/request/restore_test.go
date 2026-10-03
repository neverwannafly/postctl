package request

import (
	"strings"
	"testing"

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
		Method:   "PATCH",
		URL:      "{{baseUrl}}/new",
		Headers:  []model.KV{{Key: "Authorization", Value: hist.Mask}},
		BodyMode: model.BodyRaw,
		BodyRaw:  hist.Mask,
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

func TestApplyHistoryTemplateDisabledWithoutMatchingRequest(t *testing.T) {
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}, Deps{})
	p.SetSize(80, 24)
	_, cmd := p.Update(tui.ApplyRequestPatchMsg{RequestID: "nope", Template: hist.RequestTemplate{Method: "GET"}})
	if cmd != nil {
		t.Fatal("expected no cmd without open request")
	}
}
