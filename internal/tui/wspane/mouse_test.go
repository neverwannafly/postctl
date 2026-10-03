package wspane

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
)

func TestWSPaneMouseTabsAndWheel(t *testing.T) {
	h := newHarness(t)

	// Initial tab is tabMessages (0)
	if h.p.tab != tabMessages {
		t.Fatalf("initial tab = %v, want tabMessages", h.p.tab)
	}

	// Tab bar is on y = 2 and 3
	// Tab 1: "Params(2)" -> [13, 22)
	msg := tea.MouseClickMsg{X: 15, Y: 2, Button: tea.MouseLeft}
	h.p.HandleMouse(msg, 15, 2)
	if h.p.tab != tabParams {
		t.Errorf("tab after click Params = %v, want tabParams", h.p.tab)
	}

	// Tab 2: "Headers(1)" -> [24, 34)
	msg = tea.MouseClickMsg{X: 26, Y: 2, Button: tea.MouseLeft}
	h.p.HandleMouse(msg, 26, 2)
	if h.p.tab != tabHeaders {
		t.Errorf("tab after click Headers = %v, want tabHeaders", h.p.tab)
	}

	// Wheel scrolling in Headers tab
	h.req.Headers = append(h.req.Headers, model.KV{Key: "H1", Value: "V1"}, model.KV{Key: "H2", Value: "V2"})
	h.p.reloadAll()

	wheelDown := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}
	h.p.HandleMouse(wheelDown, 10, 10)

	wheelUp := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelUp}
	h.p.HandleMouse(wheelUp, 10, 10)
}
