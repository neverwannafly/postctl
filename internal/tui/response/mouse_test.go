package response

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

func TestResponsePaneMouseTabsAndWheel(t *testing.T) {
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, true, false), Actions: &tui.Registry{}}).(*Pane)
	p.SetSize(100, 30)

	// Initial tab is tabBody (0)
	if p.tab != tabBody {
		t.Fatalf("initial tab = %v, want tabBody", p.tab)
	}

	// Tab 0: "Body" (len 4) -> [0, 4)
	// Gap: 2
	// Tab 1: "Headers" (len 7) -> starts at col 6
	// Click on Headers tab (col 8, y 1)
	msg := tea.MouseClickMsg{X: 8, Y: 1, Button: tea.MouseLeft}
	p.HandleMouse(msg, 8, 1)
	if p.tab != tabHeaders {
		t.Errorf("tab after click Headers = %v, want tabHeaders", p.tab)
	}

	// Tab 2: "Cookies" -> starts at 6 + 7 + 2 = 15
	msg = tea.MouseClickMsg{X: 17, Y: 1, Button: tea.MouseLeft}
	p.HandleMouse(msg, 17, 1)
	if p.tab != tabCookies {
		t.Errorf("tab after click Cookies = %v, want tabCookies", p.tab)
	}

	// Wheel test
	p.rawIdx.build([]byte(strings.Repeat("line\n", 100)))
	wheelDown := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}
	p.HandleMouse(wheelDown, 10, 10)
	if p.scroll <= 0 {
		t.Errorf("wheel down should scroll down, got %d", p.scroll)
	}

	wheelUp := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelUp}
	p.HandleMouse(wheelUp, 10, 10)
	if p.scroll != 0 {
		t.Errorf("wheel up should scroll back to 0, got %d", p.scroll)
	}
}
