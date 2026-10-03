package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
