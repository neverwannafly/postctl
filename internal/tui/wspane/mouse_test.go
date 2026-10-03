package wspane

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui/vimtext"
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

// ---- the wheel and the vim fields ---------------------------------------------------

func (h *harness) wheel(down bool, x, y int) {
	h.t.Helper()
	b := tea.MouseWheelUp
	if down {
		b = tea.MouseWheelDown
	}
	h.p.HandleMouse(tea.MouseWheelMsg{X: x, Y: y, Button: b}, x, y)
}

// composerY is a row inside the composer of the Messages tab (80x24 layout).
const composerY = chrome + 2

// busy connects, fills the log and gives the composer a long text, so a wheel
// that gets through has something to move.
func busy(h *harness) {
	h.t.Helper()
	c := h.connect()
	for i := 0; i < 8; i++ {
		c.pushIn(`{"n":1}`)
	}
	h.waitLog(9)
	longComposer(h)
}

func longComposer(h *harness) string {
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = "message " + strings.Repeat("x", i%7)
	}
	text := strings.Join(lines, "\n")
	h.p.comp.Set(text)
	h.p.fitComposer()
	return text
}

func TestWheelScrollsAnUnfocusedComposerWithoutEditing(t *testing.T) {
	h := newHarness(t)
	h.key("esc") // the tab bar: the composer does not have the cursor
	before := longComposer(h)
	h.out = nil
	h.wheel(true, 5, composerY)
	h.wheel(true, 5, composerY)
	if h.p.comp.Top() == 0 {
		t.Fatal("the wheel did not scroll the composer")
	}
	down := h.p.comp.Top()
	h.wheel(false, 5, composerY)
	if h.p.comp.Top() >= down {
		t.Fatal("wheel up did not scroll back")
	}
	if h.p.comp.Text() != before || len(h.saves()) != 0 || h.p.Editing() {
		t.Fatal("the wheel edited the composer, saved, or moved the cursor into it")
	}
}

func TestWheelScrollsAComposerInNormalModeAndKeepsTheMode(t *testing.T) {
	h := newHarness(t)
	h.key("down") // the composer, normal mode
	before := longComposer(h)
	h.wheel(true, 5, composerY)
	if h.p.comp.Top() == 0 {
		t.Fatal("the wheel did not scroll the composer")
	}
	if row, _ := h.p.comp.Cursor(); row < h.p.comp.Top() {
		t.Fatal("the cursor is above the view")
	}
	if h.p.VimMode() != "NORMAL" || h.p.comp.Text() != before {
		t.Fatal("the wheel changed the mode or the text")
	}
}

// Insert and visual mode, an open cell, the log search and the template name
// prompt all own their input: the wheel does nothing and cannot commit them.
func TestWheelIsIgnoredWhileAFieldOrPromptIsBeingTyped(t *testing.T) {
	type snap struct {
		mode string
		text string
		tab  tabID
		zone zone
	}
	take := func(h *harness) snap {
		s := snap{mode: h.p.VimMode(), tab: h.p.tab, zone: h.p.zone}
		if f := h.p.activeField(); f != nil {
			s.text = f.Text()
		}
		return s
	}
	cases := []struct {
		name string
		open func(h *harness)
		want string
	}{
		{"url insert", func(h *harness) { h.key("up", "A") }, "INSERT"},
		{"composer insert", func(h *harness) { h.key("down", "i") }, "INSERT"},
		{"composer visual", func(h *harness) { h.key("down", "v") }, "VISUAL"},
		{"params cell insert", func(h *harness) { h.gotoTab(tabParams); h.key("down", "enter", "A") }, "INSERT"},
		{"header cell insert", func(h *harness) { h.gotoTab(tabHeaders); h.key("down", "enter", "A") }, "INSERT"},
		{"log search", func(h *harness) { h.key("down", "tab", "tab", "/") }, "INSERT"},
		{"template name", func(h *harness) { h.key("down", "tab", "r") }, "INSERT"},
	}
	for _, c := range cases {
		for _, pos := range []struct {
			name string
			x, y int
		}{{"over composer", 5, composerY}, {"over log", 5, 21}} {
			t.Run(c.name+"/"+pos.name, func(t *testing.T) {
				h := newHarness(t)
				busy(h)
				c.open(h)
				if got := h.p.VimMode(); got != c.want {
					t.Fatalf("setup: mode = %q, want %q", got, c.want)
				}
				before := take(h)
				l := h.p.peek()
				sel, query, top := l.log.sel, l.log.query, h.p.comp.Top()
				text := h.p.comp.Text()
				h.out = nil
				h.wheel(false, pos.x, pos.y)
				h.wheel(false, pos.x, pos.y)
				h.wheel(true, pos.x, pos.y)
				if after := take(h); after != before {
					t.Fatalf("the wheel changed the field: %+v -> %+v", before, after)
				}
				if l.log.sel != sel || l.log.query != query {
					t.Fatal("the wheel moved the log or its search")
				}
				wantScroll := pos.name == "over composer" && (c.name == "composer insert" || c.name == "composer visual")
				if wantScroll && h.p.comp.Top() == top {
					t.Fatal("the wheel did not scroll the focused composer")
				}
				if !wantScroll && h.p.comp.Top() != top {
					t.Fatal("the wheel scrolled a field other than the one under the pointer")
				}
				if h.p.comp.Text() != text {
					t.Fatal("the wheel edited the composer")
				}
				if len(h.saves()) != 0 || h.p.confirm != nil {
					t.Fatal("the wheel committed something")
				}
				if h.p.searching != (c.name == "log search") || (h.p.rename != nil) != (c.name == "template name") {
					t.Fatal("the wheel closed a prompt")
				}
			})
		}
	}
}

// A cell that is open in normal mode keeps the cursor too (M1's guard).
func TestWheelIsIgnoredWhileACellIsOpen(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabParams)
	h.key("down", "enter")
	cur := h.p.params.cur
	if !h.p.params.editing() {
		t.Fatal("setup: the cell should be open")
	}
	h.wheel(true, 5, 10)
	h.wheel(false, 5, 10)
	if h.p.params.cur != cur || !h.p.params.editing() {
		t.Fatal("the wheel moved or closed an open cell")
	}
}

// Without a field in use the wheel still moves the log and the lists.
func TestWheelStillMovesTheLogAndLists(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	for i := 0; i < 6; i++ {
		c.pushIn(`{"n":1}`)
	}
	h.waitLog(7)
	h.key("esc") // the tab bar
	l := h.p.peek()
	start := l.log.sel
	h.wheel(false, 5, 21)
	if l.log.sel == start {
		t.Fatal("the wheel over the log did not move the selection")
	}
	h.gotoTab(tabHeaders)
	h.req.Headers = append(h.req.Headers, model.KV{Key: "H1", Value: "V1"}, model.KV{Key: "H2", Value: "V2"})
	h.p.reloadAll()
	h.p.setZone(zBody)
	h.p.headers.cur = 0
	h.wheel(true, 5, 10)
	if h.p.headers.cur == 0 {
		t.Fatal("the wheel did not move the headers list")
	}
}

func TestMousePlacesCursorInURLAndComposer(t *testing.T) {
	h := newHarness(t)
	urlX := 2 + badgeW + 2
	h.p.HandleMouse(tea.MouseClickMsg{X: urlX + 2, Y: 0, Button: tea.MouseLeft}, urlX+2, 0)
	if _, col := h.p.url.Cursor(); col != 2 || h.p.VimMode() != "NORMAL" {
		t.Fatalf("URL click cursor/mode = %d/%q", col, h.p.VimMode())
	}
	h.p.comp.Set("first\nsecond")
	h.p.HandleMouse(tea.MouseClickMsg{X: 3, Y: chrome + 2, Button: tea.MouseLeft}, 3, chrome+2)
	if row, col := h.p.comp.Cursor(); row != 1 || col != 3 || h.p.VimMode() != "NORMAL" {
		t.Fatalf("composer click = %d,%d/%q", row, col, h.p.VimMode())
	}
	h.p.comp.Begin("i")
	h.advance(500 * time.Millisecond)
	h.p.HandleMouse(tea.MouseClickMsg{X: 2, Y: chrome + 1, Button: tea.MouseLeft}, 2, chrome+1)
	if h.p.comp.Mode() != vimtext.Insert {
		t.Fatal("clicking the focused composer left insert mode")
	}
}

func TestMouseKeepsURLInsertAndMasksWebSocketSecretFormYank(t *testing.T) {
	const sentinel = "mouse-hidden-websocket-sentinel"
	h := newHarness(t)
	h.p.setZone(zBar)
	h.p.url.Begin("i")
	urlX := 2 + badgeW + 2
	h.p.HandleMouse(tea.MouseClickMsg{X: urlX + 2, Y: 0, Button: tea.MouseLeft}, urlX+2, 0)
	if h.p.url.Mode() != vimtext.Insert {
		t.Fatalf("URL click changed mode to %v", h.p.url.Mode())
	}

	h.req.Auth = &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Password: sentinel}}
	h.p.reloadAll()
	h.gotoTab(tabAuth)
	h.p.setZone(zBody)
	labelW := min(20, max(10, h.p.w/3))
	valueX := 2 + labelW + 1
	y := chrome + 2
	h.p.HandleMouse(tea.MouseClickMsg{X: valueX, Y: y, Button: tea.MouseLeft}, valueX, y)
	if !h.p.auth.f.edit || h.p.auth.f.cur != 2 || h.p.auth.f.in == nil {
		t.Fatal("clicking the WebSocket password row did not open it")
	}
	h.p.auth.f.in.Begin("i")
	h.advance(500 * time.Millisecond)
	h.p.HandleMouse(tea.MouseClickMsg{X: valueX + 1, Y: y, Button: tea.MouseLeft}, valueX+1, y)
	if h.p.auth.f.in.Mode() != vimtext.Insert {
		t.Fatal("clicking the focused WebSocket password row left insert mode")
	}
	h.p.HandleMouse(tea.MouseMotionMsg{X: valueX + 4, Y: y, Button: tea.MouseLeft}, valueX+4, y)
	h.p.HandleMouse(tea.MouseReleaseMsg{X: valueX + 4, Y: y, Button: tea.MouseLeft}, valueX+4, y)
	res := h.p.auth.f.in.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	h.run(res.Cmd)
	if strings.Contains(h.p.View(), sentinel) {
		t.Fatal("hidden WebSocket password was revealed in the frame")
	}
	if len(h.clip) != 1 || h.clip[0] != h.theme.Glyphs.Mask || strings.Contains(h.clip[0], sentinel) {
		t.Fatal("hidden WebSocket password yank did not produce the fixed mask")
	}
}
