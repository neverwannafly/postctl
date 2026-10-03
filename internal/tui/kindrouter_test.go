package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// probePane records what the shell gives it.
type probePane struct {
	name    string
	sel     *model.Request
	selects int
	keys    []string
	other   []string
	focus   int
	blur    int
	editing bool
	w, h    int
}

type probeMsg struct{ text string }

func (p *probePane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	switch m := msg.(type) {
	case SelectionMsg:
		p.sel = m.Ref.Request
		p.selects++
	case tea.KeyPressMsg:
		p.keys = append(p.keys, m.String())
	default:
		p.other = append(p.other, fmt.Sprintf("%T", msg))
	}
	return p, nil
}

func (p *probePane) View() string {
	name := "none"
	if p.sel != nil {
		name = p.sel.Name
	}
	return p.name + " pane: " + name
}
func (p *probePane) SetSize(w, h int) { p.w, p.h = w, h }
func (p *probePane) Focus() tea.Cmd   { p.focus++; return nil }
func (p *probePane) Blur()            { p.blur++ }
func (p *probePane) KeyHints() []ui.Hint {
	return []ui.Hint{{Key: "K", Label: p.name + " hint", Priority: 1}}
}
func (p *probePane) Editing() bool { return p.editing }
func (p *probePane) Title() string { return p.name + " title" }
func (p *probePane) Actions() []Action {
	return []Action{{ID: p.name + ".act", Title: p.name + " action", Group: "Request", Run: func() tea.Cmd { return nil }}}
}

func wsDemo() *model.Workspace {
	ws := demo()
	c := ws.Collections[0]
	c.Items = append(c.Items, model.NewWebSocketRequest("Live feed", "wss://feed.example.test/stream"))
	return ws
}

func routed(t *testing.T) (*harness, *probePane, *probePane) {
	return routedAt(t, 160, 30)
}

func routedAt(t *testing.T, w, height int) (*harness, *probePane, *probePane) {
	t.Helper()
	httpP, wsP := &probePane{name: "http"}, &probePane{name: "ws"}
	h := newHarness(t, wsDemo(), w, height, func(o *Options) {
		o.NewRequestPane = func(PaneEnv) Pane { return httpP }
		o.NewPaneForKind = map[model.Kind]PaneFactory{model.KindWebSocket: func(PaneEnv) Pane { return wsP }}
	})
	return h, httpP, wsP
}

func TestRouterShowsThePaneForTheSelectedKind(t *testing.T) {
	h, httpP, wsP := routed(t)
	h.selectByName("Create charge")
	if httpP.sel == nil || httpP.sel.Name != "Create charge" || wsP.sel != nil {
		t.Fatalf("http selection %v, ws selection %v", httpP.sel, wsP.sel)
	}
	h.mustHave("http pane: Create charge")

	h.selectByName("Live feed")
	if wsP.sel == nil || wsP.sel.Name != "Live feed" {
		t.Fatalf("ws pane was not given the WebSocket request: %v", wsP.sel)
	}
	if httpP.sel != nil {
		t.Fatalf("the HTTP pane must let go of its request, still has %q", httpP.sel.Name)
	}
	h.mustHave("ws pane: Live feed")
	h.mustNotHave("http pane")
	h.mustHave("ws title")

	h.selectByName("Create charge")
	if wsP.sel != nil || httpP.sel == nil {
		t.Fatalf("switching back: http %v ws %v", httpP.sel, wsP.sel)
	}
	h.mustHave("http pane: Create charge")
}

func TestRouterSendsKeysOnlyToTheActivePane(t *testing.T) {
	h, httpP, wsP := routed(t)
	h.selectByName("Live feed")
	h.keys("enter") // open: focus moves to the request pane
	if h.a.focus != SlotRequest {
		t.Fatalf("focus = %v", h.a.focus)
	}
	h.keys("x", "y")
	if got := strings.Join(wsP.keys, ","); got != "x,y" {
		t.Fatalf("ws keys = %q", got)
	}
	if len(httpP.keys) != 0 {
		t.Fatalf("http keys = %q", httpP.keys)
	}
	if wsP.focus == 0 {
		t.Fatal("the active pane must receive Focus")
	}
	h.mustHave("ws hint")
	wsP.editing = true
	if !h.a.editing() {
		t.Fatal("Editing must come from the active pane")
	}
}

func TestRouterFocusFollowsAPaneSwitchWhileFocused(t *testing.T) {
	h, httpP, wsP := routed(t)
	h.selectByName("Create charge")
	h.keys("enter")
	if httpP.focus == 0 {
		t.Fatal("http pane not focused")
	}
	// The user goes back to the tree and opens the WebSocket request.
	h.keys("alt+1")
	h.selectByName("Live feed")
	h.keys("enter")
	if wsP.focus == 0 || h.a.focus != SlotRequest {
		t.Fatalf("ws focus %d, shell focus %v", wsP.focus, h.a.focus)
	}
	h.keys("z")
	if len(wsP.keys) != 1 || wsP.keys[0] != "z" {
		t.Fatalf("ws keys = %q", wsP.keys)
	}
}

func TestRouterBroadcastsOtherMessagesToEveryPane(t *testing.T) {
	h, httpP, wsP := routed(t)
	h.selectByName("Live feed")
	h.msg(probeMsg{"hi"})
	for _, p := range []*probePane{httpP, wsP} {
		found := false
		for _, o := range p.other {
			if strings.HasSuffix(o, "probeMsg") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s pane missed a private message: %v", p.name, p.other)
		}
	}
}

func TestRouterSizesEveryPane(t *testing.T) {
	h, httpP, wsP := routed(t)
	h.msg(sizeMsg(120, 40))
	if httpP.w == 0 || httpP.w != wsP.w || httpP.h != wsP.h {
		t.Fatalf("sizes: http %dx%d ws %dx%d", httpP.w, httpP.h, wsP.w, wsP.h)
	}
}

func TestWebSocketUsesTheResponseSpaceAndSkipsItsFocus(t *testing.T) {
	for _, width := range []int{120, 160, 200} {
		h, _, wsP := routedAt(t, width, 40)
		h.selectByName("Live feed")
		h.keys("enter")
		if h.a.responseHidden() != (width >= 100) {
			t.Fatalf("width %d: responseHidden=%v", width, h.a.responseHidden())
		}
		if width >= 100 {
			if strings.Contains(strings.Split(h.screen(), "\n")[2], "Response") {
				t.Fatalf("width %d still shows Response:\n%s", width, h.screen())
			}
			if wsP.w != h.a.geo().requestW {
				t.Fatalf("width %d: WebSocket width=%d, geometry=%d", width, wsP.w, h.a.geo().requestW)
			}
			h.keys("tab")
			if h.a.focus != SlotSidebar {
				t.Fatalf("width %d: tab should skip Response, focus=%v", width, h.a.focus)
			}
			h.keys("3")
			if h.a.focus == SlotResponse {
				t.Fatalf("width %d: hidden Response accepted focus", width)
			}
		}
	}
}

func TestWebSocketLayoutGoldens(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
	}{{"140x40", 140, 40}, {"200x60", 200, 60}} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, _ := routedAt(t, tc.w, tc.h)
			h.selectByName("Live feed")
			h.keys("enter")
			golden(t, "websocket-layout-"+tc.name, h.screen())
		})
	}
}

func TestRouterActionsAreTheUnion(t *testing.T) {
	h, _, _ := routed(t)
	var ids []string
	for _, a := range h.a.registry.All() {
		ids = append(ids, a.ID)
	}
	all := strings.Join(ids, " ")
	if !strings.Contains(all, "http.act") || !strings.Contains(all, "ws.act") {
		t.Fatalf("actions = %s", all)
	}
}

func TestWebSocketBadgeInTreeAndPalette(t *testing.T) {
	h, _, _ := routed(t)
	h.mustHave("WS")
	line := ""
	for _, l := range strings.Split(h.screen(), "\n") {
		if strings.Contains(l, "Live feed") {
			line = l
		}
	}
	if !strings.Contains(line, "WS") {
		t.Fatalf("tree row = %q", line)
	}
	h.keys("ctrl+p")
	h.typeText("live")
	found := false
	for _, l := range strings.Split(h.screen(), "\n") {
		if strings.Contains(l, "Live feed") && strings.Contains(l, "WS") {
			found = true
		}
	}
	if !found {
		t.Fatalf("palette lacks the WS badge:\n%s", h.screen())
	}
}

func TestNewWebSocketRequestFromThePalette(t *testing.T) {
	h, _, wsP := routed(t)
	h.keys("ctrl+p")
	h.typeText("new websocket")
	h.keys("enter")
	h.mustHave("New WebSocket request")
	h.typeText("Orders stream")
	h.keys("enter")

	var made *model.Request
	_ = h.a.ws.Collections[0].Walk(func(it model.Item, _ model.Container, _ int) error {
		if r, ok := it.(*model.Request); ok && r.Name == "Orders stream" {
			made = r
		}
		return nil
	})
	if made == nil || made.Kind != model.KindWebSocket {
		t.Fatalf("created request = %+v", made)
	}
	if wsP.sel == nil || wsP.sel.ID != made.ID {
		t.Fatalf("the new request must open in the WebSocket pane: %v", wsP.sel)
	}
	if h.a.focus != SlotRequest {
		t.Fatalf("focus = %v", h.a.focus)
	}
	if len(h.be.saved) == 0 {
		t.Fatal("the collection was not saved")
	}
}

func TestNewPromptOffersWebSocketOnlyWithAPane(t *testing.T) {
	h, _, _ := routed(t)
	h.keys("space", "a")
	h.mustHave("WebSocket")
	h.overlayDismiss()

	plain := newHarness(t, demo(), 160, 30)
	plain.keys("space", "a")
	plain.mustNotHave("WebSocket")
	plain.overlayDismiss()
	plain.keys("ctrl+p")
	plain.typeText("websocket")
	plain.mustNotHave("New WebSocket request")
}

func TestNewPromptCreatesWebSocketKind(t *testing.T) {
	h, _, _ := routed(t)
	h.keys("space", "a")
	h.keys("tab") // Request -> WebSocket
	h.typeText("Prices")
	h.keys("enter")
	var made *model.Request
	_ = h.a.ws.Collections[0].Walk(func(it model.Item, _ model.Container, _ int) error {
		if r, ok := it.(*model.Request); ok && r.Name == "Prices" {
			made = r
		}
		return nil
	})
	if made == nil || made.Kind != model.KindWebSocket {
		t.Fatalf("created request = %+v", made)
	}
}

func TestWithoutFactoriesThereIsNoRouter(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	if _, ok := h.a.request.(*kindRouter); ok {
		t.Fatal("a router is only built when NewPaneForKind is set")
	}
}
