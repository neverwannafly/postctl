package request

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
)

func TestRequestPaneMouseTabsAndWheel(t *testing.T) {
	h := newHarness(t)
	// Initial tab is tabParams (0)
	if h.p.tab != tabParams {
		t.Fatalf("initial tab = %v, want tabParams", h.p.tab)
	}

	// Tab bar is on y = 2 and 3
	// Tab 0: Params (w ~ 8)
	// Gap: 2
	// Tab 1: Auth (col 10)
	// Gap: 2
	// Tab 2: Headers (col 17)
	// Click on Auth tab (col 12, y 2)
	msg := tea.MouseClickMsg{X: 12, Y: 2, Button: tea.MouseLeft}
	h.p.HandleMouse(msg, 12, 2)
	if h.p.tab != tabAuth {
		t.Errorf("tab after click Auth = %v, want tabAuth", h.p.tab)
	}

	// Click on Headers tab (col 18, y 2)
	msg = tea.MouseClickMsg{X: 18, Y: 2, Button: tea.MouseLeft}
	h.p.HandleMouse(msg, 18, 2)
	if h.p.tab != tabHeaders {
		t.Errorf("tab after click Headers = %v, want tabHeaders", h.p.tab)
	}

	// Wheel scrolling in Headers tab
	// Add some header rows
	h.req.Headers = append(h.req.Headers, model.KV{Key: "H1", Value: "V1"}, model.KV{Key: "H2", Value: "V2"})
	h.p.reloadAll()

	wheelDown := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}
	h.p.HandleMouse(wheelDown, 10, 10)

	wheelUp := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelUp}
	h.p.HandleMouse(wheelUp, 10, 10)
}

func TestRequestWheelDoesNothingWhileEditing(t *testing.T) {
	h := newHarness(t)
	// Focus on URL editing
	h.p.urlE = true
	if !h.p.Editing() {
		t.Fatalf("expected p.Editing() to be true")
	}

	// Send wheel
	wheelDown := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}
	h.p.HandleMouse(wheelDown, 10, 10)

	// URL editor must still be active
	if !h.p.urlE {
		t.Errorf("wheel while editing should not dismiss or commit URL edit")
	}

	// Now test KV edit
	h.p.urlE = false
	kv, ok := h.p.views[tabParams].(*kvView)
	if !ok {
		t.Fatalf("expected kvView for tabParams")
	}
	items := kv.items(h.p)
	if len(items) < 2 {
		t.Fatalf("need at least 2 items, got %d", len(items))
	}
	kv.cur = 1
	kv.beginEdit(h.p, items, 0)
	if !h.p.Editing() {
		t.Fatalf("expected p.Editing() to be true during kv edit")
	}

	// Send wheel up: with the current bug, kv.cur moves from 1 to 0!
	wheelUp := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelUp}
	h.p.HandleMouse(wheelUp, 10, 10)
	if kv.cur != 1 {
		t.Errorf("wheel up while editing moved kv.cur from 1 to %d", kv.cur)
	}
}
