package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

func mouseClick(x, y int, btn tea.MouseButton) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: btn}
}

func mouseWheel(x, y int, btn tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{X: x, Y: y, Button: btn}
}

type fakeMousePane struct {
	Pane
	clicked bool
	wheeled bool
	lastX   int
	lastY   int
}

func (f *fakeMousePane) HandleMouse(msg tea.MouseMsg, x, y int) tea.Cmd {
	m := msg.Mouse()
	switch m.Button {
	case tea.MouseLeft:
		f.clicked = true
	case tea.MouseWheelUp, tea.MouseWheelDown:
		f.wheeled = true
	}
	f.lastX = x
	f.lastY = y
	return nil
}

func (f *fakeMousePane) Focus() tea.Cmd                     { return nil }
func (f *fakeMousePane) Blur()                              {}
func (f *fakeMousePane) View() string                       { return "fake mouse pane" }
func (f *fakeMousePane) SetSize(w, h int)                   {}
func (f *fakeMousePane) KeyHints() []ui.Hint                { return nil }
func (f *fakeMousePane) Update(msg tea.Msg) (Pane, tea.Cmd) { return f, nil }

// TestHitTestingAcrossLayouts verifies hit regions computed in ThreePane, TwoPane, and SinglePane layouts.
func TestHitTestingAcrossLayouts(t *testing.T) {
	tests := []struct {
		name       string
		w, h       int
		wantLayout ui.Layout
	}{
		{"SinglePane_80x24", 80, 24, ui.LayoutSingle},
		{"TwoPane_100x30", 100, 30, ui.LayoutTwoPane},
		{"ThreePane_140x40", 140, 40, ui.LayoutThreePane},
		{"ThreePane_200x60", 200, 60, ui.LayoutThreePane},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hs := newHarness(t, demo(), tt.w, tt.h)
			g := hs.a.geo()
			if g.layout != tt.wantLayout {
				t.Fatalf("layout = %v, want %v", g.layout, tt.wantLayout)
			}
			r := hs.a.hitRegions()
			if r.sidebar.w <= 0 || r.sidebar.h <= 0 {
				t.Errorf("sidebar hit region invalid: %+v", r.sidebar)
			}
			switch tt.wantLayout {
			case ui.LayoutThreePane:
				if r.request.w <= 0 || r.response.w <= 0 {
					t.Errorf("three-pane request/response hit regions invalid: req=%+v, resp=%+v", r.request, r.response)
				}
				if r.sidebar.x+r.sidebar.w >= r.request.x {
					t.Errorf("sidebar overlaps request: sidebar=%+v, request=%+v", r.sidebar, r.request)
				}
				if r.request.x+r.request.w >= r.response.x {
					t.Errorf("request overlaps response: request=%+v, response=%+v", r.request, r.response)
				}
			case ui.LayoutTwoPane:
				if r.switcher.w <= 0 || r.switcher.h <= 0 {
					t.Errorf("two-pane switcher hit region invalid: %+v", r.switcher)
				}
			default:
				if r.switcher.w <= 0 || r.switcher.h <= 0 {
					t.Errorf("single-pane switcher hit region invalid: %+v", r.switcher)
				}
			}
		})
	}
}

// TestClickFocus verifies clicking panes or switcher titles changes focus appropriately.
func TestClickFocus(t *testing.T) {
	t.Run("ThreePane", func(t *testing.T) {
		hs := newHarness(t, demo(), 140, 40)
		r := hs.a.hitRegions()

		// Initial focus is sidebar
		if hs.a.focus != SlotSidebar {
			t.Fatalf("initial focus = %v, want %v", hs.a.focus, SlotSidebar)
		}

		// Click inside request pane
		hs.msg(mouseClick(r.request.x+5, r.request.y+5, tea.MouseLeft))
		if hs.a.focus != SlotRequest {
			t.Errorf("focus after clicking request = %v, want %v", hs.a.focus, SlotRequest)
		}

		// Click inside response pane
		hs.msg(mouseClick(r.response.x+5, r.response.y+5, tea.MouseLeft))
		if hs.a.focus != SlotResponse {
			t.Errorf("focus after clicking response = %v, want %v", hs.a.focus, SlotResponse)
		}

		// Click inside sidebar title
		hs.msg(mouseClick(r.sidebarTitle.x+2, r.sidebarTitle.y, tea.MouseLeft))
		if hs.a.focus != SlotSidebar {
			t.Errorf("focus after clicking sidebar title = %v, want %v", hs.a.focus, SlotSidebar)
		}
	})

	t.Run("TwoPaneSwitcher", func(t *testing.T) {
		hs := newHarness(t, demo(), 100, 30)
		r := hs.a.hitRegions()

		// Focus Request via switcher
		hs.msg(mouseClick(r.switcher.x+2, r.switcher.y, tea.MouseLeft))
		if hs.a.focus != SlotRequest {
			t.Errorf("focus after clicking switcher request = %v, want %v", hs.a.focus, SlotRequest)
		}

		// Focus Response via switcher (right side of switcher)
		hs.msg(mouseClick(r.switcher.x+18, r.switcher.y, tea.MouseLeft))
		if hs.a.focus != SlotResponse {
			t.Errorf("focus after clicking switcher response = %v, want %v", hs.a.focus, SlotResponse)
		}
	})

	t.Run("SinglePaneSwitcher", func(t *testing.T) {
		hs := newHarness(t, demo(), 80, 24)
		r := hs.a.hitRegions()

		// Click Request on single-pane switcher
		hs.msg(mouseClick(r.switcher.x+22, r.switcher.y, tea.MouseLeft))
		if hs.a.focus != SlotRequest {
			t.Errorf("focus after clicking single-pane request = %v, want %v", hs.a.focus, SlotRequest)
		}

		// Click Response on single-pane switcher
		hs.msg(mouseClick(r.switcher.x+38, r.switcher.y, tea.MouseLeft))
		if hs.a.focus != SlotResponse {
			t.Errorf("focus after clicking single-pane response = %v, want %v", hs.a.focus, SlotResponse)
		}

		// Click Sidebar on single-pane switcher
		hs.msg(mouseClick(r.switcher.x+5, r.switcher.y, tea.MouseLeft))
		if hs.a.focus != SlotSidebar {
			t.Errorf("focus after clicking single-pane sidebar = %v, want %v", hs.a.focus, SlotSidebar)
		}
	})
}

// TestTreeMouseInteraction verifies tree selection, double-click, and fold toggle.
func TestTreeMouseInteraction(t *testing.T) {
	hs := newHarness(t, demo(), 140, 40)
	r := hs.a.hitRegions()

	fakeTime := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	hs.a.now = func() time.Time { return fakeTime }

	// First row (index 0) in demo is collection "HTTPBin APIs"
	row0 := hs.a.tree.rows[0]
	if !row0.open {
		t.Fatalf("row 0 should start open in demo")
	}

	// Click on fold glyph of row 0 to toggle it closed
	// Glyph is at column 2 (marker is 2 wide, depth 0)
	hs.msg(mouseClick(r.sidebar.x+2, r.sidebar.y, tea.MouseLeft))
	if hs.a.tree.rows[0].open {
		t.Errorf("fold glyph click should toggle container closed")
	}

	// Click fold glyph again to toggle open
	hs.msg(mouseClick(r.sidebar.x+2, r.sidebar.y, tea.MouseLeft))
	if !hs.a.tree.rows[0].open {
		t.Errorf("fold glyph click should toggle container open")
	}

	// Click row 1 to select it
	row1 := hs.a.tree.rows[1]
	hs.msg(mouseClick(r.sidebar.x+10, r.sidebar.y+1, tea.MouseLeft))
	if hs.a.tree.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", hs.a.tree.cursor)
	}

	// Click row 1 again (click on already-selected row) -> opens it
	// If it's a request, opening it focuses SlotRequest
	if row1.kind == rowRequest {
		hs.msg(mouseClick(r.sidebar.x+10, r.sidebar.y+1, tea.MouseLeft))
		if hs.a.focus != SlotRequest {
			t.Errorf("clicking already-selected request row should open and focus request, got focus=%v", hs.a.focus)
		}
	}

	// Test double-click: select row 2 with double-click <= 400ms
	hs.msg(mouseClick(r.sidebarTitle.x+2, r.sidebarTitle.y, tea.MouseLeft)) // focus sidebar
	if len(hs.a.tree.rows) > 2 && hs.a.tree.rows[2].kind == rowRequest {
		// First click at t=0
		fakeTime = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
		hs.msg(mouseClick(r.sidebar.x+10, r.sidebar.y+2, tea.MouseLeft))
		if hs.a.tree.cursor != 2 {
			t.Fatalf("cursor = %d, want 2 after first click", hs.a.tree.cursor)
		}
		// Reset focus to sidebar to ensure double-click actually triggered openRow
		hs.a.focus = SlotSidebar

		// Second click at t=200ms (<= 400ms)
		fakeTime = fakeTime.Add(200 * time.Millisecond)
		hs.msg(mouseClick(r.sidebar.x+10, r.sidebar.y+2, tea.MouseLeft))
		if hs.a.focus != SlotRequest {
			t.Errorf("double-click within 200ms should open request, got focus=%v", hs.a.focus)
		}
	}
}

// TestPaneMouseDelegation verifies mouse clicks and wheels are forwarded to MouseHandler panes.
func TestPaneMouseDelegation(t *testing.T) {
	reqP := &fakeMousePane{}
	respP := &fakeMousePane{}

	hs := newHarness(t, demo(), 140, 40, func(o *Options) {
		o.NewRequestPane = func(PaneEnv) Pane { return reqP }
		o.NewResponsePane = func(PaneEnv) Pane { return respP }
	})
	r := hs.a.hitRegions()

	// Click on Request pane delegates to reqP
	hs.msg(mouseClick(r.request.x+10, r.request.y+5, tea.MouseLeft))
	if !reqP.clicked {
		t.Errorf("request pane must receive click via HandleMouse")
	}
	if reqP.lastX != 10 || reqP.lastY != 5 {
		t.Errorf("request relative coords = (%d, %d), want (10, 5)", reqP.lastX, reqP.lastY)
	}

	// Click on Response pane delegates to respP
	hs.msg(mouseClick(r.response.x+12, r.response.y+7, tea.MouseLeft))
	if !respP.clicked {
		t.Errorf("response pane must receive click via HandleMouse")
	}
	if respP.lastX != 12 || respP.lastY != 7 {
		t.Errorf("response relative coords = (%d, %d), want (12, 7)", respP.lastX, respP.lastY)
	}
}

// TestWheelRouting verifies wheel scrolling does not change pane focus.
func TestWheelRouting(t *testing.T) {
	reqP := &fakeMousePane{}
	respP := &fakeMousePane{}

	hs := newHarness(t, demo(), 140, 40, func(o *Options) {
		o.NewRequestPane = func(PaneEnv) Pane { return reqP }
		o.NewResponsePane = func(PaneEnv) Pane { return respP }
	})
	r := hs.a.hitRegions()

	// Focus is sidebar
	if hs.a.focus != SlotSidebar {
		t.Fatalf("initial focus = %v, want %v", hs.a.focus, SlotSidebar)
	}

	// Wheel over Request pane
	hs.msg(mouseWheel(r.request.x+5, r.request.y+5, tea.MouseWheelDown))
	if hs.a.focus != SlotSidebar {
		t.Errorf("wheel over request must not change focus, got %v", hs.a.focus)
	}
	if !reqP.wheeled {
		t.Errorf("request pane must receive wheel event")
	}

	// Wheel over Response pane
	hs.msg(mouseWheel(r.response.x+5, r.response.y+5, tea.MouseWheelDown))
	if hs.a.focus != SlotSidebar {
		t.Errorf("wheel over response must not change focus, got %v", hs.a.focus)
	}
	if !respP.wheeled {
		t.Errorf("response pane must receive wheel event")
	}

	// Wheel over Sidebar scrolls tree
	initialTop := hs.a.tree.top
	hs.msg(mouseWheel(r.sidebar.x+5, r.sidebar.y+5, tea.MouseWheelDown))
	if hs.a.focus != SlotSidebar {
		t.Errorf("wheel over sidebar must keep focus, got %v", hs.a.focus)
	}
	hs.msg(mouseWheel(r.sidebar.x+5, r.sidebar.y+5, tea.MouseWheelUp))
	if hs.a.tree.top != initialTop {
		t.Errorf("tree top = %d, want %d", hs.a.tree.top, initialTop)
	}
}

// TestOverlayMouseRules verifies modal dialogs ignore outside clicks while non-modal overlays close.
func TestOverlayMouseRules(t *testing.T) {
	t.Run("NonModalOverlayClosesOnOutsideClick", func(t *testing.T) {
		hs := newHarness(t, demo(), 140, 40)
		// Open help overlay (?)
		hs.msg(tea.KeyPressMsg{Code: '?'})
		if hs.a.ov == nil {
			t.Fatalf("help overlay should be open")
		}

		// Click outside the overlay body (e.g. on the header line y=0)
		hs.msg(mouseClick(10, 0, tea.MouseLeft))
		if hs.a.ov != nil {
			t.Errorf("clicking outside non-modal overlay must close it, got ov=%v", hs.a.ov)
		}
	})

	t.Run("ModalDialogIgnoresOutsideClick", func(t *testing.T) {
		hs := newHarness(t, demo(), 140, 40)
		// Trigger a confirmation dialog
		hs.a.ov = &confirmOverlay{
			c: ui.NewConfirm("Delete", "Are you sure?", "Delete", true),
		}
		if hs.a.ov == nil {
			t.Fatalf("confirm dialog should be open")
		}
		if d, ok := hs.a.ov.(dialog); !ok || !d.isDialog() {
			t.Fatalf("confirm overlay must report isDialog() = true")
		}

		// Click far away outside the dialog box (e.g. at (0, 0))
		hs.msg(mouseClick(0, 0, tea.MouseLeft))
		if hs.a.ov == nil {
			t.Errorf("clicking outside modal dialog must be ignored, but dialog was closed")
		}
	})
}

// TestMouseDisabledOptionAndEnv verifies NoMouse and POSTCTL_NO_MOUSE.
func TestMouseDisabledOptionAndEnv(t *testing.T) {
	// 1. Mouse enabled by default
	hs := newHarness(t, demo(), 140, 40)
	v := hs.a.View()
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("default View.MouseMode = %v, want MouseModeCellMotion", v.MouseMode)
	}

	// 2. NoMouse option disables mouse
	hsOff := newHarness(t, demo(), 140, 40, func(o *Options) {
		o.NoMouse = true
	})
	vOff := hsOff.a.View()
	if vOff.MouseMode != tea.MouseModeNone {
		t.Errorf("NoMouse View.MouseMode = %v, want MouseModeNone", vOff.MouseMode)
	}

	// Clicking when disabled does nothing
	rOff := hsOff.a.hitRegions()
	hsOff.msg(mouseClick(rOff.request.x+5, rOff.request.y+5, tea.MouseLeft))
	if hsOff.a.focus != SlotSidebar {
		t.Errorf("mouse message when disabled must not change focus, got %v", hsOff.a.focus)
	}

	// 3. POSTCTL_NO_MOUSE env
	t.Setenv("POSTCTL_NO_MOUSE", "1")
	opts := ApplyEnv(Options{})
	if !opts.NoMouse {
		t.Errorf("POSTCTL_NO_MOUSE=1 must set NoMouse=true in ApplyEnv")
	}
}

// TestViewPurityWithMouse verifies View() remains a pure function.
func TestViewPurityWithMouse(t *testing.T) {
	hs := newHarness(t, demo(), 140, 40)
	v1 := hs.a.View()
	v2 := hs.a.View()
	if v1.Content != v2.Content {
		t.Errorf("consecutive View() calls produced different content")
	}
	if v1.MouseMode != v2.MouseMode {
		t.Errorf("consecutive View() calls produced different MouseMode")
	}
}

// TestMouseReleaseAndMotionIgnored verifies release and drag motion events do not trigger clicks.
func TestMouseReleaseAndMotionIgnored(t *testing.T) {
	hs := newHarness(t, demo(), 140, 40)
	r := hs.a.hitRegions()

	// 1. In tree: select first selectable row
	rowY := r.sidebar.y + 1
	hs.msg(tea.MouseClickMsg{X: r.sidebar.x + 5, Y: rowY, Button: tea.MouseLeft})
	if hs.a.tree.cursor < 0 {
		t.Fatalf("row should be selected")
	}
	// Focus is sidebar
	hs.a.focus = SlotSidebar

	// Sending MouseReleaseMsg on the same row must NOT trigger open (selected row branch)
	hs.msg(tea.MouseReleaseMsg{X: r.sidebar.x + 5, Y: rowY, Button: tea.MouseLeft})
	if hs.a.focus != SlotSidebar {
		t.Errorf("MouseReleaseMsg must NOT trigger row open / focus change, got %v", hs.a.focus)
	}

	// Sending MouseMotionMsg with MouseLeft must NOT trigger row open or re-click
	hs.msg(tea.MouseMotionMsg{X: r.sidebar.x + 5, Y: rowY, Button: tea.MouseLeft})
	if hs.a.focus != SlotSidebar {
		t.Errorf("MouseMotionMsg must NOT trigger row open / focus change, got %v", hs.a.focus)
	}

	// 2. Non-modal overlay: release outside must NOT close overlay
	hs.msg(tea.KeyPressMsg{Code: '?'})
	if hs.a.ov == nil {
		t.Fatalf("help overlay should be open")
	}
	// Send release outside overlay
	hs.msg(tea.MouseReleaseMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if hs.a.ov == nil {
		t.Errorf("MouseReleaseMsg outside overlay must NOT close overlay")
	}
	// Send motion outside overlay
	hs.msg(tea.MouseMotionMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if hs.a.ov == nil {
		t.Errorf("MouseMotionMsg outside overlay must NOT close overlay")
	}
}

// TestScreenOverlayModalForOutsideClick verifies full-screen screens are treated as modal for outside clicks.
func TestScreenOverlayModalForOutsideClick(t *testing.T) {
	hs := newHarness(t, demo(), 140, 40)
	hs.a.ov = &screenOverlay{s: &fakeScreen{}}
	if hs.a.ov == nil {
		t.Fatalf("screen overlay should be set")
	}

	// Click outside content body (e.g. at header line y=0)
	hs.msg(tea.MouseClickMsg{X: 10, Y: 0, Button: tea.MouseLeft})
	if hs.a.ov == nil {
		t.Errorf("outside click on full-screen screen overlay must be ignored, but overlay was closed")
	}
}

// TestSinglePaneSwitcherWithEachPaneFocused verifies dynamic range hit testing in SinglePane layout.
func TestSinglePaneSwitcherWithEachPaneFocused(t *testing.T) {
	hs := newHarness(t, demo(), 80, 24)
	if hs.a.layoutKind() != ui.LayoutSingle {
		t.Fatalf("expected LayoutSingle at 80 cols")
	}
	r := hs.a.hitRegions()

	// Test each focus state and verify clicking all 3 tabs
	for _, focus := range []Slot{SlotSidebar, SlotRequest, SlotResponse} {
		hs.a.focus = focus
		title := hs.a.switcherTitle(r.switcher.w, []Slot{SlotSidebar, SlotRequest, SlotResponse})
		clean := ansi.Strip(title)
		if clean == "" {
			t.Fatalf("switcherTitle empty for focus %v", focus)
		}

		// Click Sidebar tab
		idx1 := strings.Index(clean, "[1]")
		if idx1 < 0 {
			t.Fatalf("could not find [1] in clean switcherTitle: %q", clean)
		}
		hs.msg(tea.MouseClickMsg{X: r.switcher.x + idx1 + 1, Y: r.switcher.y, Button: tea.MouseLeft})
		if hs.a.focus != SlotSidebar {
			t.Errorf("focus %v: clicking [1] should focus Sidebar, got %v", focus, hs.a.focus)
		}

		// Click Request tab
		hs.a.focus = focus
		title = hs.a.switcherTitle(r.switcher.w, []Slot{SlotSidebar, SlotRequest, SlotResponse})
		clean = ansi.Strip(title)
		idx2 := strings.Index(clean, "[2]")
		if idx2 < 0 {
			t.Fatalf("could not find [2] in clean switcherTitle: %q", clean)
		}
		hs.msg(tea.MouseClickMsg{X: r.switcher.x + idx2 + 1, Y: r.switcher.y, Button: tea.MouseLeft})
		if hs.a.focus != SlotRequest {
			t.Errorf("focus %v: clicking [2] should focus Request, got %v", focus, hs.a.focus)
		}

		// Click Response tab
		hs.a.focus = focus
		title = hs.a.switcherTitle(r.switcher.w, []Slot{SlotSidebar, SlotRequest, SlotResponse})
		clean = ansi.Strip(title)
		idx3 := strings.Index(clean, "[3]")
		if idx3 < 0 {
			t.Fatalf("could not find [3] in clean switcherTitle: %q", clean)
		}
		hs.msg(tea.MouseClickMsg{X: r.switcher.x + idx3 + 1, Y: r.switcher.y, Button: tea.MouseLeft})
		if hs.a.focus != SlotResponse {
			t.Errorf("focus %v: clicking [3] should focus Response, got %v", focus, hs.a.focus)
		}
	}
}

// TestTreeWheelKeepsSelectionVisible verifies tree wheel scrolling keeps cursor visible (vim Ctrl-e/Ctrl-y).
func TestTreeWheelKeepsSelectionVisible(t *testing.T) {
	c := model.NewCollection("Big Collection")
	for i := 0; i < 50; i++ {
		req := model.NewRequest("Request "+strings.Repeat("a", i), "GET", "http://example.test")
		c.Items = append(c.Items, req)
	}
	ws := &model.Workspace{ID: model.NewID(), Name: "big", Collections: []*model.Collection{c}}
	hs := newHarness(t, ws, 80, 24)
	r := hs.a.hitRegions()

	// Ensure collection is expanded
	hs.a.tree.setOpen(c.ID, true)
	hs.a.tree.rebuild()
	hs.a.tree.cursor = 0
	hs.a.tree.top = 0

	// Wheel down several times
	for i := 0; i < 5; i++ {
		hs.msg(tea.MouseWheelMsg{X: r.sidebar.x + 5, Y: r.sidebar.y + 5, Button: tea.MouseWheelDown})
	}

	if hs.a.tree.top <= 0 {
		t.Fatalf("expected tree.top to have scrolled > 0, got %d", hs.a.tree.top)
	}
	visibleRows := hs.a.tree.viewRows()
	if hs.a.tree.cursor < hs.a.tree.top {
		t.Errorf("cursor (%d) moved above top (%d), fell off-screen", hs.a.tree.cursor, hs.a.tree.top)
	}
	if hs.a.tree.cursor >= hs.a.tree.top+visibleRows {
		t.Errorf("cursor (%d) moved below visible window (top %d + %d)", hs.a.tree.cursor, hs.a.tree.top, visibleRows)
	}
}

// TestMouseBeforePhaseReadyAndClearPendingLeader verifies mouse ignores before ready and clears pending leader.
func TestMouseBeforePhaseReadyAndClearPendingLeader(t *testing.T) {
	hs := newHarness(t, demo(), 140, 40)
	r := hs.a.hitRegions()

	// 1. Mouse before ready is ignored
	hs.a.phase = phaseLoading
	hs.msg(tea.MouseClickMsg{X: r.request.x + 5, Y: r.request.y + 5, Button: tea.MouseLeft})
	if hs.a.focus != SlotSidebar {
		t.Errorf("mouse before phaseReady must be ignored, got focus %v", hs.a.focus)
	}

	// 2. Click clears pending leader/prefix state
	hs.a.phase = phaseReady
	hs.a.pend = &pendingKeys{prefix: " "}
	hs.msg(tea.MouseClickMsg{X: r.request.x + 5, Y: r.request.y + 5, Button: tea.MouseLeft})
	if hs.a.pend != nil {
		t.Errorf("click must clear pending leader/prefix state (a.pend), got %v", hs.a.pend)
	}
}

// TestMatrixAndBoundaries tests 120x40, 99 and 139 boundaries, ASCII look, sidebar toggled.
func TestMatrixAndBoundaries(t *testing.T) {
	matrix := []struct {
		name       string
		w, h       int
		ascii      bool
		wantLayout ui.Layout
	}{
		{"120x40_TwoPane", 120, 40, false, ui.LayoutTwoPane},
		{"99x30_Boundary_Single", 99, 30, false, ui.LayoutSingle},
		{"139x40_Boundary_TwoPane", 139, 40, false, ui.LayoutTwoPane},
		{"140x40_Boundary_ThreePane", 140, 40, false, ui.LayoutThreePane},
		{"ASCII_80x24", 80, 24, true, ui.LayoutSingle},
		{"ASCII_120x40", 120, 40, true, ui.LayoutTwoPane},
		{"ASCII_140x40", 140, 40, true, ui.LayoutThreePane},
	}

	for _, tt := range matrix {
		t.Run(tt.name, func(t *testing.T) {
			hs := newHarness(t, demo(), tt.w, tt.h, func(o *Options) {
				o.ASCII = tt.ascii
			})
			if hs.a.layoutKind() != tt.wantLayout {
				t.Errorf("layout = %v, want %v", hs.a.layoutKind(), tt.wantLayout)
			}
			r := hs.a.hitRegions()
			if r.sidebar.w <= 0 || r.sidebar.h <= 0 {
				t.Errorf("invalid sidebar region: %+v", r.sidebar)
			}

			// Test sidebar toggle (Space b)
			hs.a.toggleSidebar()
			rToggled := hs.a.hitRegions()
			if tt.wantLayout == ui.LayoutSingle {
				if hs.a.focus == SlotSidebar && rToggled.sidebar.w <= 0 {
					t.Errorf("expected sidebar visible when focused in single pane")
				} else if hs.a.focus != SlotSidebar && rToggled.request.w <= 0 && rToggled.response.w <= 0 {
					t.Errorf("expected request or response visible in single pane")
				}
			} else {
				if rToggled.sidebar.w <= 0 {
					t.Errorf("expected sidebar visible in wide layout after toggle")
				}
			}
		})
	}
}

type trackingDialog struct {
	viewCalls int
}

func (d *trackingDialog) isDialog() bool                                { return true }
func (d *trackingDialog) setSize(w, h int)                              {}
func (d *trackingDialog) update(a *app, msg tea.Msg) (overlay, tea.Cmd) { return d, nil }
func (d *trackingDialog) view(a *app, w, h int) string                  { d.viewCalls++; return "tracking dialog" }
func (d *trackingDialog) hints(a *app) []ui.Hint                        { return nil }

// TestNonClickNonWheelEarlyReturn verifies motion and release do not re-render overlays or compute hitRegions.
func TestNonClickNonWheelEarlyReturn(t *testing.T) {
	hs := newHarness(t, demo(), 140, 40)
	td := &trackingDialog{}
	hs.a.ov = td

	// Motion event should return early without rendering overlay view
	hs.msg(tea.MouseMotionMsg{X: 10, Y: 10, Button: tea.MouseLeft})
	if td.viewCalls != 0 {
		t.Errorf("MouseMotionMsg should return early without calling view, got %d calls", td.viewCalls)
	}

	// Release event should also return early
	hs.msg(tea.MouseReleaseMsg{X: 10, Y: 10, Button: tea.MouseLeft})
	if td.viewCalls != 0 {
		t.Errorf("MouseReleaseMsg should return early without calling view, got %d calls", td.viewCalls)
	}

	// Click event does hitRegions and thus calls view
	hs.msg(tea.MouseClickMsg{X: 10, Y: 10, Button: tea.MouseLeft})
	if td.viewCalls == 0 {
		t.Errorf("MouseClickMsg should compute hitRegions and invoke view")
	}
}
