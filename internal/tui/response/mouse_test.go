package response

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
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

func TestResponseCollapsedTabsAndHistory(t *testing.T) {
	// 1. Collapsed mode at 80 cols (with response result and history, tab width > 70 + 10 > 80 cols)
	p := Factory(Deps{
		Log: func() (tui.HistoryReader, error) { return nil, nil },
	})(tui.PaneEnv{Theme: ui.NewTheme(true, true, false), Actions: &tui.Registry{}}).(*Pane)
	p.SetSize(80, 24)
	p.ref.Request = &model.Request{ID: "r1"}
	p.applyResult(tui.SendResult{
		RequestID: "r1",
		Response: &httpx.Response{
			StatusCode: 200,
			Headers:    make([]model.KV, 12),
			Cookies:    make([]model.Cookie, 5),
		},
		Tests:   []tui.TestOutcome{{Passed: true}, {Passed: true}, {Passed: true}},
		Console: []tui.ConsoleLine{{Level: "warn", Text: "w1"}, {Level: "error", Text: "e1"}},
	})
	p.hist.loaded = true
	p.hist.items = make([]history.Entry, 20)
	t.Logf("tabsBar at 80:\n%s", p.tabsBar(80))

	// In collapsed mode, clicking at y=2 (body content) must NOT switch tabs
	p.HandleMouse(tea.MouseClickMsg{X: 8, Y: 2, Button: tea.MouseLeft}, 8, 2)
	if p.tab != tabBody {
		t.Errorf("clicking y=2 in collapsed mode must NOT switch tabs, got %v", p.tab)
	}

	// Click at y=1 on the visible selector advances to next tab
	p.HandleMouse(tea.MouseClickMsg{X: 2, Y: 1, Button: tea.MouseLeft}, 2, 1)
	if p.tab != tabHeaders {
		t.Errorf("clicking visible selector at y=1 should advance to Headers, got %v", p.tab)
	}

	// 2. V3 History tab hit-testing
	pWithHist := Factory(Deps{
		Log: func() (tui.HistoryReader, error) { return nil, nil },
	})(tui.PaneEnv{Theme: ui.NewTheme(true, true, false), Actions: &tui.Registry{}}).(*Pane)
	pWithHist.SetSize(140, 40)
	tabs := pWithHist.buildTabs()
	histIdx := len(tabs) - 1
	if tabs[histIdx].Label != "History" {
		t.Fatalf("expected last tab to be History, got %q", tabs[histIdx].Label)
	}
	ranges := pWithHist.th.TabRanges(pWithHist.w, tabs)
	histRange := ranges[histIdx]
	clickX := (histRange.Start + histRange.End) / 2
	pWithHist.HandleMouse(tea.MouseClickMsg{X: clickX, Y: 1, Button: tea.MouseLeft}, clickX, 1)
	if pWithHist.tab != tabHistory {
		t.Errorf("clicking History tab should switch to tabHistory, got %v", pWithHist.tab)
	}
}
