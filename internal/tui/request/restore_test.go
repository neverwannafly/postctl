package request

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	hist "github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
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
	dir := t.TempDir()
	ws := tuitest.Demo()
	if _, err := tuitest.Write(dir, ws); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir, store.WithSecretStore(store.NewMemorySecrets()))
	if err != nil {
		t.Fatal(err)
	}
	coll := ws.Collections[0]
	var req *model.Request
	for _, r := range coll.Requests() {
		if r.Name == "Create charge" {
			req = r
		}
	}
	if req == nil {
		t.Fatal("create charge missing")
	}
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}, Deps{
		After: testAfterImmediate,
	})
	p.SetSize(120, 40)
	_, _ = p.Update(tui.WorkspaceReloadedMsg{Workspace: ws})
	_, _ = p.Update(tui.SelectionMsg{Ref: tui.RequestRef{Collection: coll, Request: req, Path: []string{coll.Name, req.Name}}})
	tmpl := hist.RequestTemplate{Method: "TRACE", URL: req.URL.Raw}
	_, cmd := p.Update(tui.ApplyRequestPatchMsg{RequestID: req.ID, Template: tmpl})
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	for cmd != nil {
		var msg tea.Msg
		if c := cmd(); c != nil {
			if batch, ok := c.(tea.BatchMsg); ok {
				for _, sub := range batch {
					if sub != nil {
						msg = sub()
					}
				}
			} else {
				msg = c
			}
		}
		if sm, ok := msg.(tui.SaveMsg); ok {
			c := ws.Collection(sm.Collection)
			if err := st.SaveRequest(context.Background(), c, sm.ID); err != nil {
				t.Fatal(err)
			}
		}
		if msg != nil {
			_, cmd = p.Update(msg)
		} else {
			cmd = nil
		}
	}
	reloaded, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	var saved *model.Request
	for _, r := range reloaded.Collections[0].Requests() {
		if r.ID == req.ID {
			saved = r
		}
	}
	if saved == nil || saved.Method != "TRACE" {
		t.Fatalf("persisted method = %v", saved)
	}
}

func TestPendingPatchCancelledWhenSwitchingRequests(t *testing.T) {
	ws := tuitest.Demo()
	coll := ws.Collections[0]
	var create, list, refund *model.Request
	for _, r := range coll.Requests() {
		switch r.Name {
		case "Create charge":
			create = r
		case "List charges":
			list = r
		case "Refund charge":
			refund = r
		}
	}
	if create == nil || list == nil || refund == nil {
		t.Fatal("demo requests missing")
	}
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}, Deps{After: testAfterImmediate})
	p.SetSize(120, 40)
	_, _ = p.Update(tui.WorkspaceReloadedMsg{Workspace: ws})
	_, _ = p.Update(tui.SelectionMsg{Ref: tui.RequestRef{Collection: coll, Request: list, Path: []string{coll.Name, list.Name}}})
	tmpl := hist.RequestTemplate{Method: "OPTIONS", URL: create.URL.Raw}
	_, _ = p.Update(tui.ApplyRequestPatchMsg{RequestID: create.ID, Template: tmpl})
	if len(p.pending) != 1 {
		t.Fatalf("pending = %d", len(p.pending))
	}
	_, _ = p.Update(tui.SelectionMsg{Ref: tui.RequestRef{Collection: coll, Request: refund, Path: []string{coll.Name, refund.Name}}})
	if !strings.Contains(p.notice, "Restore cancelled") {
		t.Fatalf("notice = %q", p.notice)
	}
}

func testAfterImmediate(_ time.Duration, msg tea.Msg) tea.Cmd {
	if _, ok := msg.(noticeExpireMsg); ok {
		return nil
	}
	return func() tea.Msg { return msg }
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
