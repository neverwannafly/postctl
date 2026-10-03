package request

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
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
	kv.beginEdit(h.p, items, 0, "")
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

// ---- the wheel and the vim fields ---------------------------------------------------

func longText(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line " + strings.Repeat("x", i%5)
	}
	return strings.Join(lines, "\n")
}

func (h *harness) wheel(down bool) {
	h.t.Helper()
	b := tea.MouseWheelUp
	if down {
		b = tea.MouseWheelDown
	}
	h.p.HandleMouse(tea.MouseWheelMsg{X: 10, Y: 10, Button: b}, 10, 10)
}

// wheelCase puts a long text in one kind of editor.
type wheelCase struct {
	name string
	open func(h *harness) // the cursor on the tab bar, the editor not focused
	into func(h *harness) // the cursor in the editor, normal mode
	ed   func(h *harness) *vimfield.Field
	text func(h *harness) string
}

func wheelCases() []wheelCase {
	return []wheelCase{
		{
			name: "pre-request script",
			open: func(h *harness) {
				h.req.Scripts.PreRequest = &model.Script{Source: longText(80)}
				h.p.reloadAll()
				h.gotoTab(tabPre)
			},
			into: func(h *harness) { h.key("down") },
			ed:   func(h *harness) *vimfield.Field { return h.p.views[tabPre].(*scriptView).ed },
			text: func(h *harness) string { return h.req.Scripts.PreRequest.Source },
		},
		{
			name: "test script",
			open: func(h *harness) {
				h.req.Scripts.Test = &model.Script{Source: longText(80)}
				h.p.reloadAll()
				h.gotoTab(tabTests)
			},
			into: func(h *harness) { h.key("down") },
			ed:   func(h *harness) *vimfield.Field { return h.p.views[tabTests].(*scriptView).ed },
			text: func(h *harness) string { return h.req.Scripts.Test.Source },
		},
		{
			name: "raw body",
			open: func(h *harness) {
				h.req.Body = model.Body{Mode: model.BodyRaw, Language: "text", Raw: longText(80)}
				h.p.reloadAll()
				h.gotoTab(tabBody)
			},
			into: func(h *harness) { h.key("down", "down", "down") },
			ed:   func(h *harness) *vimfield.Field { return h.p.views[tabBody].(*bodyView).raw },
			text: func(h *harness) string { return h.req.Body.Raw },
		},
		{
			name: "graphql query",
			open: func(h *harness) {
				h.req.Body = model.Body{Mode: model.BodyGraphQL, GraphQL: &model.GraphQLBody{Query: longText(80)}}
				h.p.reloadAll()
				h.gotoTab(tabBody)
			},
			into: func(h *harness) { h.key("down", "down") },
			ed:   func(h *harness) *vimfield.Field { return h.p.views[tabBody].(*bodyView).gq },
			text: func(h *harness) string { return h.req.Body.GraphQL.Query },
		},
	}
}

// The wheel over a script or body editor that does not have the cursor scrolls
// its view: no edit, no save, no mode change.
func TestWheelScrollsAnUnfocusedEditorWithoutEditing(t *testing.T) {
	for _, tc := range wheelCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.open(h)
			if h.p.Editing() {
				t.Fatal("the cursor should be on the tab bar")
			}
			ed, before := tc.ed(h), tc.text(h)
			nSaves, nDirty := len(h.saves()), 0
			if n, _ := h.dirty(); n > 0 {
				nDirty = n
			}
			h.wheel(true)
			h.wheel(true)
			if ed.Top() == 0 {
				t.Fatal("the wheel did not scroll the editor")
			}
			down := ed.Top()
			h.wheel(false)
			if ed.Top() >= down {
				t.Fatalf("wheel up did not scroll back (top %d -> %d)", down, ed.Top())
			}
			if tc.text(h) != before || ed.Text() != before {
				t.Fatal("the wheel edited the text")
			}
			if n, _ := h.dirty(); n != nDirty || len(h.saves()) != nSaves {
				t.Fatal("the wheel marked the request changed")
			}
			if h.p.Editing() || h.p.zone == zBody {
				t.Fatal("the wheel moved the cursor into the editor")
			}
		})
	}
}

// In normal mode the editor has the cursor but is not being typed into: the
// wheel scrolls the view, keeps the cursor inside it, and keeps the mode.
func TestWheelScrollsAFocusedEditorInNormalMode(t *testing.T) {
	for _, tc := range wheelCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.open(h)
			tc.into(h)
			ed := tc.ed(h)
			if !h.p.Editing() || ed.ModeName() != "NORMAL" {
				t.Fatalf("setup: editing=%v mode=%q", h.p.Editing(), ed.ModeName())
			}
			before := tc.text(h)
			h.wheel(true)
			h.wheel(true)
			if ed.Top() == 0 {
				t.Fatal("the wheel did not scroll the editor")
			}
			if row, _ := ed.Cursor(); row < ed.Top() {
				t.Fatalf("cursor row %d is above the view at %d", row, ed.Top())
			}
			if ed.ModeName() != "NORMAL" || tc.text(h) != before || !h.p.Editing() {
				t.Fatal("the wheel changed the mode or the text")
			}
		})
	}
}

// The wheel over the focused field scrolls it in insert and visual mode
// without editing it or changing mode.
func TestWheelScrollsFocusedEditorInInsertOrVisualMode(t *testing.T) {
	for _, tc := range wheelCases() {
		for _, mode := range []struct{ name, key, want string }{{"insert", "i", "INSERT"}, {"visual", "v", "VISUAL"}} {
			t.Run(tc.name+"/"+mode.name, func(t *testing.T) {
				h := newHarness(t)
				tc.open(h)
				tc.into(h)
				h.key(mode.key)
				ed := tc.ed(h)
				if ed.ModeName() != mode.want {
					t.Fatalf("mode = %q, want %q", ed.ModeName(), mode.want)
				}
				top := ed.Top()
				before := tc.text(h)
				nSaves := len(h.saves())
				h.wheel(true)
				h.wheel(false)
				h.wheel(true)
				if ed.Top() == top {
					t.Fatal("the wheel did not scroll the focused field")
				}
				if ed.ModeName() != mode.want || tc.text(h) != before || len(h.saves()) != nSaves {
					t.Fatal("the wheel changed the mode, the text or the saves")
				}
			})
		}
	}
}

func TestWheelOnlyScrollsGraphQLFieldUnderPointer(t *testing.T) {
	h := newHarness(t)
	h.req.Body = model.Body{Mode: model.BodyGraphQL, GraphQL: &model.GraphQLBody{
		Query: longText(80), Variables: longText(80),
	}}
	h.p.reloadAll()
	h.gotoTab(tabBody)
	b := h.p.views[tabBody].(*bodyView)
	h.p.setZone(zBody)
	b.cur = 1
	b.focusZone(h.p, zkQuery, "")
	_, bodyH := h.p.bodySize()
	qh, _ := gqHeights(max(1, bodyH-2))
	before := b.gq.Top()
	y := chrome + 3 + qh
	h.p.HandleMouse(tea.MouseWheelMsg{X: 2, Y: y, Button: tea.MouseWheelDown}, 2, y)
	if b.gq.Top() != before {
		t.Fatal("wheel over GraphQL variables scrolled the query field")
	}
}

// A cell being edited and the URL field in insert mode are left alone too.
func TestWheelIsIgnoredWhileTheURLIsInInsertMode(t *testing.T) {
	h := newHarness(t)
	h.key("up", "A") // the URL field, insert mode
	if !h.p.url.Typing() {
		t.Fatal("setup: the URL field should be in insert mode")
	}
	before := h.p.url.Text()
	h.wheel(true)
	h.wheel(false)
	if !h.p.url.Typing() || h.p.url.Text() != before || h.p.zone != zBar {
		t.Fatal("the wheel disturbed the URL field")
	}
}

// Editing is true exactly when a vim field has the cursor, in any mode.
func TestEditingMeansAVimFieldHasTheCursor(t *testing.T) {
	h := newHarness(t)
	h.req.Scripts.PreRequest = &model.Script{Source: "a\nb"}
	h.p.reloadAll()
	h.gotoTab(tabPre)
	if h.p.Editing() {
		t.Fatal("Editing on the tab bar")
	}
	h.key("down")
	if !h.p.Editing() {
		t.Fatal("not Editing with the cursor in the editor (normal mode)")
	}
	h.key("i")
	if !h.p.Editing() {
		t.Fatal("not Editing in insert mode")
	}
	h.key("esc", "esc")
	if h.p.Editing() {
		t.Fatal("still Editing after leaving the editor")
	}
	h.key("up", "up", "A") // the tab bar, then the URL field
	if !h.p.Editing() {
		t.Fatal("not Editing in the URL field")
	}
}

// ---- a click on another pane while typing ----------------------------------------

// Clicking another pane while a field is in insert mode commits the edit: the
// pane blurs, the pending change is written, and nothing typed is lost.
func TestClickAwayFromAFieldInInsertModeSavesTheEdit(t *testing.T) {
	// Row 2 of the screen is the pane title row: the sidebar title is at the
	// left, the response title in the right column.
	for _, target := range []struct {
		name string
		x    int
	}{{"sidebar", 3}, {"response", 110}} {
		t.Run(target.name, func(t *testing.T) {
			r := newShellRig(t, "")
			r.keys("g", "u", "A")
			r.mustHave("INSERT")
			for _, k := range []string{"z", "q"} {
				r.keys(k)
			}
			r.send(tea.MouseClickMsg{X: target.x, Y: 2, Button: tea.MouseLeft})
			r.pump(200 * time.Millisecond)
			if strings.Contains(r.screen(), "INSERT") {
				t.Fatal("the field is still in insert mode after the click")
			}

			st, err := store.Open(r.dir, store.WithSecretStore(store.NewMemorySecrets()))
			if err != nil {
				t.Fatal(err)
			}
			ws, err := st.Load()
			if err != nil {
				t.Fatal(err)
			}
			var saved *model.Request
			for _, req := range ws.Collections[0].Requests() {
				if req.Name == "Create charge" {
					saved = req
				}
			}
			if saved == nil || !strings.HasSuffix(saved.URL.Raw, "zq") {
				t.Fatal("the edit was not written when focus left the pane")
			}

			// Back in the pane the edit is still there, exactly once.
			r.keys("2")
			if got := strings.Count(r.screen(), "zq"); got != 1 {
				t.Fatalf("the typed text shows %d times after coming back", got)
			}
		})
	}
}

func TestMousePlacesURLCursorAndDragsScriptSelection(t *testing.T) {
	h := newHarness(t)
	urlX := 2 + methodW + 2
	h.p.HandleMouse(tea.MouseClickMsg{X: urlX + 3, Y: 0, Button: tea.MouseLeft}, urlX+3, 0)
	if row, col := h.p.url.Cursor(); row != 0 || col != 3 || h.p.url.ModeName() != "NORMAL" {
		t.Fatalf("URL click = cursor %d,%d mode %q", row, col, h.p.url.ModeName())
	}

	h.req.Scripts.PreRequest = &model.Script{Source: "alpha beta\ngamma"}
	h.p.reloadAll()
	h.gotoTab(tabPre)
	h.p.HandleMouse(tea.MouseClickMsg{X: 3, Y: chrome, Button: tea.MouseLeft}, 3, chrome)
	if !h.p.mouse.Active() {
		t.Fatal("script press did not start")
	}
	if row, col := h.p.views[tabPre].(*scriptView).ed.Cursor(); row != 0 || col != 1 {
		t.Fatalf("press cursor = %d,%d", row, col)
	}
	if _, _, ok := h.p.views[tabPre].(*scriptView).ed.PosAt(6, 1); !ok {
		t.Fatal("second script row is not mapped")
	}
	motion := tea.MouseMotionMsg{X: 6, Y: chrome + 1, Button: tea.MouseLeft}
	if motion.Mouse().Button != tea.MouseLeft {
		t.Fatal("motion does not report held left button")
	}
	h.p.HandleMouse(motion, 6, chrome+1)
	if got := h.p.views[tabPre].(*scriptView).ed.ModeName(); got != "VISUAL" {
		row, col := h.p.views[tabPre].(*scriptView).ed.Cursor()
		t.Fatalf("motion mode/cursor = %q/%d,%d, want VISUAL", got, row, col)
	}
	h.p.HandleMouse(tea.MouseReleaseMsg{X: 99, Y: 99, Button: tea.MouseLeft}, 99, 99)
	ed := h.p.views[tabPre].(*scriptView).ed
	if ed.ModeName() != "VISUAL" {
		t.Fatalf("drag mode = %q, want VISUAL", ed.ModeName())
	}
	if h.p.mouse.Active() {
		t.Fatal("release outside did not end drag")
	}
}

func TestMouseOpensKVCellAtPointer(t *testing.T) {
	h := newHarness(t)
	kv := h.p.views[tabParams].(*kvView)
	h.p.HandleMouse(tea.MouseClickMsg{X: 6, Y: chrome + 1, Button: tea.MouseLeft}, 6, chrome+1)
	if !kv.edit || kv.col != 0 || kv.in == nil {
		t.Fatal("clicking the first key cell did not open it")
	}
	if row, col := kv.in.Cursor(); row != 0 || col != 0 || kv.in.ModeName() != "NORMAL" {
		t.Fatalf("KV click = cursor %d,%d mode %q", row, col, kv.in.ModeName())
	}
	kv.in.Begin("i")
	h.advance(500 * time.Millisecond)
	h.p.HandleMouse(tea.MouseClickMsg{X: 7, Y: chrome + 1, Button: tea.MouseLeft}, 7, chrome+1)
	if kv.in.Mode() != vimtext.Insert {
		t.Fatal("clicking the open key cell left insert mode")
	}
}

func TestScriptEdgeDragScrollsOneLineAndViewStaysPure(t *testing.T) {
	h := newHarness(t)
	h.req.Scripts.PreRequest = &model.Script{Source: longText(40)}
	h.p.reloadAll()
	h.gotoTab(tabPre)
	script := h.p.views[tabPre].(*scriptView)
	script.enter(h.p)
	script.size(h.p)
	script.ed.SetCursor(12, 0)
	h.p.HandleMouse(tea.MouseClickMsg{X: 6, Y: chrome + 1, Button: tea.MouseLeft}, 6, chrome+1)
	start := script.ed.Top()
	for i := 1; i <= 3; i++ {
		h.p.HandleMouse(tea.MouseMotionMsg{X: 6, Y: h.p.h + 2, Button: tea.MouseLeft}, 6, h.p.h+2)
		if got := script.ed.Top(); got != start+i {
			t.Fatalf("down motion %d moved top from %d to %d", i, start, got)
		}
		if i == 1 {
			row, col := script.ed.Cursor()
			mode, active := script.ed.Mode(), h.p.mouse.Active()
			first, second := h.p.View(), h.p.View()
			if first != second || !active || !h.p.mouse.Active() {
				t.Fatal("View mutated pane drag state")
			}
			if r, c := script.ed.Cursor(); r != row || c != col || script.ed.Mode() != mode {
				t.Fatal("View mutated the script selection")
			}
		}
	}
	for i := 1; i <= 3; i++ {
		h.p.HandleMouse(tea.MouseMotionMsg{X: 6, Y: chrome - 1, Button: tea.MouseLeft}, 6, chrome-1)
		if got := script.ed.Top(); got != start+3-i {
			t.Fatalf("up motion %d moved top from %d to %d", i, start+3, got)
		}
	}
}

func TestMouseClickKeepsFocusedURLInInsertMode(t *testing.T) {
	h := newHarness(t)
	h.p.setZone(zBar)
	h.p.url.Begin("i")
	urlX := 2 + methodW + 2
	h.p.HandleMouse(tea.MouseClickMsg{X: urlX + 2, Y: 0, Button: tea.MouseLeft}, urlX+2, 0)
	if h.p.url.Mode() != vimtext.Insert {
		t.Fatalf("URL click changed mode to %v", h.p.url.Mode())
	}
}

func TestMouseOpensFormRowAndMasksHiddenYanks(t *testing.T) {
	const sentinel = "mouse-hidden-form-sentinel"
	h := newHarness(t)
	h.req.Auth = &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Password: sentinel}}
	h.p.reloadAll()
	h.gotoTab(tabAuth)
	h.p.setZone(zBody)
	labelW := min(20, max(10, h.p.w/3))
	valueX := 2 + labelW + 1
	h.p.HandleMouse(tea.MouseClickMsg{X: valueX, Y: chrome + 1, Button: tea.MouseLeft}, valueX, chrome+1)
	auth := h.p.views[tabAuth].(*authView)
	if !auth.f.edit || auth.f.cur != 1 || auth.f.in == nil {
		t.Fatal("clicking an auth text row did not open its editor")
	}
	auth.f.in.Begin("i")
	h.advance(500 * time.Millisecond)
	h.p.HandleMouse(tea.MouseClickMsg{X: valueX + 1, Y: chrome + 1, Button: tea.MouseLeft}, valueX+1, chrome+1)
	if auth.f.in.Mode() != vimtext.Insert {
		t.Fatal("clicking the focused auth row left insert mode")
	}
	auth.f.edit = false
	secretY := chrome + 2
	h.p.HandleMouse(tea.MouseClickMsg{X: valueX, Y: secretY, Button: tea.MouseLeft}, valueX, secretY)
	h.p.HandleMouse(tea.MouseMotionMsg{X: valueX + 4, Y: secretY, Button: tea.MouseLeft}, valueX+4, secretY)
	h.p.HandleMouse(tea.MouseReleaseMsg{X: valueX + 4, Y: secretY, Button: tea.MouseLeft}, valueX+4, secretY)
	res := auth.f.in.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	h.run(res.Cmd)
	if strings.Contains(h.p.View(), sentinel) || len(h.clip) != 1 || h.clip[0] != h.theme.Glyphs.Mask {
		t.Fatal("hidden auth yank did not produce the fixed mask")
	}

	h.req.Headers[0].Key = "Authorization"
	h.p.reloadAll()
	h.gotoTab(tabHeaders)
	kv := h.p.views[tabHeaders].(*kvView)
	items := kv.items(h.p)
	keyW := 0
	for _, it := range items {
		keyW = max(keyW, ui.Width(it.Key))
	}
	keyW = min(max(keyW, 3), max(8, (h.p.w-6)*35/100))
	headerValueX := 6 + keyW + ui.SpaceSM
	headerY := chrome + 1
	h.p.HandleMouse(tea.MouseClickMsg{X: headerValueX, Y: headerY, Button: tea.MouseLeft}, headerValueX, headerY)
	if kv.in == nil {
		t.Fatal("clicking a secret header value did not open its editor")
	}
	kv.in.Begin("i")
	h.advance(500 * time.Millisecond)
	statusY := h.p.h - 1
	h.p.HandleMouse(tea.MouseClickMsg{X: 13, Y: statusY, Button: tea.MouseLeft}, 13, statusY)
	if kv.in.Mode() != vimtext.Insert {
		t.Fatal("clicking the focused header field left insert mode")
	}
	h.p.HandleMouse(tea.MouseReleaseMsg{X: 13, Y: statusY, Button: tea.MouseLeft}, 13, statusY)
	h.p.HandleMouse(tea.MouseClickMsg{X: headerValueX, Y: headerY, Button: tea.MouseLeft}, headerValueX, headerY)
	h.p.HandleMouse(tea.MouseMotionMsg{X: headerValueX + 4, Y: headerY, Button: tea.MouseLeft}, headerValueX+4, headerY)
	h.p.HandleMouse(tea.MouseReleaseMsg{X: headerValueX + 4, Y: headerY, Button: tea.MouseLeft}, headerValueX+4, headerY)
	res = kv.in.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	h.run(res.Cmd)
	if strings.Contains(h.p.View(), h.req.Headers[0].Value) {
		t.Fatal("hidden header was revealed in the frame")
	}
	if len(h.clip) != 2 || h.clip[1] != h.theme.Glyphs.Mask {
		t.Fatal("hidden header yank did not produce the fixed mask")
	}
}
