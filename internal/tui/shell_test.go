package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

func TestLayoutSwitchesAtBreakpoints(t *testing.T) {
	for _, c := range []struct {
		width int
		want  ui.Layout
	}{
		{200, ui.LayoutThreePane}, {140, ui.LayoutThreePane}, {139, ui.LayoutTwoPane},
		{100, ui.LayoutTwoPane}, {99, ui.LayoutSingle}, {80, ui.LayoutSingle},
	} {
		h := newHarness(t, demo(), c.width, 30)
		if got := h.a.layoutKind(); got != c.want {
			t.Errorf("width %d: layout %v, want %v", c.width, got, c.want)
		}
		scr := h.screen()
		switch c.want {
		case ui.LayoutThreePane:
			if !strings.Contains(scr, "Response") || strings.Count(strings.Split(scr, "\n")[2], "│") != 2 {
				t.Errorf("width %d: expected three panes:\n%s", c.width, scr)
			}
		case ui.LayoutTwoPane:
			if strings.Count(strings.Split(scr, "\n")[2], "│") != 1 || !strings.Contains(scr, "2 Request") {
				t.Errorf("width %d: expected two panes:\n%s", c.width, scr)
			}
		default:
			if strings.Contains(strings.Split(scr, "\n")[3], "│") || !strings.Contains(scr, "1 Collections") {
				t.Errorf("width %d: expected a single pane:\n%s", c.width, scr)
			}
		}
		for i, line := range strings.Split(scr, "\n") {
			if ui.Width(line) > c.width {
				t.Errorf("width %d: line %d is %d cells wide", c.width, i, ui.Width(line))
			}
		}
		if n := len(strings.Split(scr, "\n")); n != 30 {
			t.Errorf("width %d: view is %d rows, want 30", c.width, n)
		}
	}
}

func TestFocusCycleThreePane(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	if h.a.focus != SlotSidebar {
		t.Fatalf("initial focus %v", h.a.focus)
	}
	h.keys("tab")
	if h.a.focus != SlotRequest {
		t.Fatalf("tab -> %v", h.a.focus)
	}
	h.keys("tab")
	if h.a.focus != SlotResponse {
		t.Fatalf("tab -> %v", h.a.focus)
	}
	h.keys("tab")
	if h.a.focus != SlotSidebar {
		t.Fatalf("tab wraps -> %v", h.a.focus)
	}
	h.keys("shift+tab")
	if h.a.focus != SlotResponse {
		t.Fatalf("shift+tab -> %v", h.a.focus)
	}
	h.keys("1")
	if h.a.focus != SlotSidebar {
		t.Fatalf("1 -> %v", h.a.focus)
	}
	h.keys("3")
	if h.a.focus != SlotResponse {
		t.Fatalf("3 -> %v", h.a.focus)
	}
	h.keys("2")
	if h.a.focus != SlotRequest {
		t.Fatalf("2 -> %v", h.a.focus)
	}
	h.keys("alt+3")
	if h.a.focus != SlotResponse {
		t.Fatalf("alt+3 -> %v", h.a.focus)
	}
	h.keys("alt+1")
	if h.a.focus != SlotSidebar {
		t.Fatalf("alt+1 -> %v", h.a.focus)
	}
}

func TestFocusIsVisibleWithoutColor(t *testing.T) {
	h := newHarness(t, demo(), 160, 30, func(o *Options) { o.NoColor = true })
	if !strings.Contains(strings.Split(h.screen(), "\n")[2], "› Collections") {
		t.Fatalf("focused sidebar needs a marker:\n%s", h.screen())
	}
	h.keys("tab")
	if !strings.Contains(strings.Split(h.screen(), "\n")[2], "› Request") {
		t.Fatalf("focused request pane needs a marker:\n%s", h.screen())
	}
}

func TestSinglePaneSidebarOverlay(t *testing.T) {
	h := newHarness(t, demo(), 80, 24)
	h.selectByName("Create charge")
	h.keys("enter") // open: closes the overlay, focuses the request
	if h.a.focus != SlotRequest {
		t.Fatalf("focus after open = %v", h.a.focus)
	}
	h.mustHave("POST")
	h.mustHave("{{baseUrl}}/v1/charges")
	h.mustNotHave("Refund charge") // sidebar is hidden

	h.keys("ctrl+b")
	if h.a.focus != SlotSidebar {
		t.Fatalf("ctrl+b -> %v", h.a.focus)
	}
	h.mustHave("Refund charge")
	h.keys("esc")
	if h.a.focus != SlotRequest {
		t.Fatalf("esc closes the overlay, focus = %v", h.a.focus)
	}
	h.keys("tab")
	if h.a.focus != SlotResponse {
		t.Fatalf("tab in single pane -> %v", h.a.focus)
	}
	h.keys("tab")
	if h.a.focus != SlotRequest {
		t.Fatalf("tab toggles request/response, got %v", h.a.focus)
	}
	h.keys("/")
	if h.a.focus != SlotSidebar || !h.a.tree.filtering {
		t.Fatalf("/ opens the sidebar filter: focus=%v filtering=%v", h.a.focus, h.a.tree.filtering)
	}
}

func TestTwoPaneTabSwitchesRightPane(t *testing.T) {
	h := newHarness(t, demo(), 110, 30)
	h.selectByName("Create charge")
	h.keys("enter")
	h.mustHave("Payments › Charges › Create charge")
	h.keys("tab")
	if h.a.focus != SlotResponse {
		t.Fatalf("focus = %v", h.a.focus)
	}
	h.mustHave("No response yet")
	h.mustNotHave("Payments › Charges › Create charge")
	h.keys("tab", "tab")
	if h.a.focus != SlotRequest {
		t.Fatalf("focus = %v", h.a.focus)
	}
}

// textPane is a pane with a text field, to prove the shell does not steal keys.
type textPane struct {
	got     []string
	editing bool
	w, h    int
}

func (p *textPane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		p.got = append(p.got, k.String())
	}
	return p, nil
}
func (p *textPane) View() string     { return "text pane" }
func (p *textPane) SetSize(w, h int) { p.w, p.h = w, h }
func (p *textPane) Focus() tea.Cmd   { return nil }
func (p *textPane) Blur()            {}
func (p *textPane) KeyHints() []ui.Hint {
	return []ui.Hint{{Key: "Ctrl-Enter", Label: "Send", Priority: 0}}
}
func (p *textPane) Editing() bool { return p.editing }
func (p *textPane) Title() string { return "Editor" }
func (p *textPane) Actions() []Action {
	return []Action{{ID: "request.send", Title: "Send request", Group: "Request", Keys: "Ctrl-Enter", Run: func() tea.Cmd {
		p.got = append(p.got, "ACTION")
		return nil
	}}}
}

func TestEditingPaneKeepsPlainKeys(t *testing.T) {
	tp := &textPane{}
	h := newHarness(t, demo(), 160, 30, func(o *Options) {
		o.NewRequestPane = func(PaneEnv) Pane { return tp }
	})
	h.keys("2")
	tp.editing = true
	h.keys("E", "q", "1", "?", "/", "tab")
	want := "E q 1 ? / tab"
	if got := strings.Join(tp.got, " "); got != want {
		t.Fatalf("pane got %q, want %q", got, want)
	}
	if h.quit || h.a.ov != nil || h.a.focus != SlotRequest {
		t.Fatal("shell must not react to keys while a text field has focus")
	}
	// Shortcuts that never conflict with typing still work.
	h.keys("ctrl+p")
	if h.a.ov == nil {
		t.Fatal("ctrl+p opens the palette even while typing")
	}
	h.keys("esc")
	h.keys("alt+1")
	if h.a.focus != SlotSidebar {
		t.Fatalf("alt+1 jumps even while typing, focus=%v", h.a.focus)
	}
	tp.editing = false
	h.keys("2")
	h.keys("E")
	if _, ok := h.a.ov.(*envOverlay); !ok {
		t.Fatalf("E opens the switcher once typing stops, ov=%T", h.a.ov)
	}
}

func TestPaneTitleAndHintsAreUsed(t *testing.T) {
	tp := &textPane{}
	h := newHarness(t, demo(), 160, 30, func(o *Options) { o.NewRequestPane = func(PaneEnv) Pane { return tp } })
	h.keys("2")
	h.mustHave("Editor")
	h.mustHave("Ctrl-Enter Send")
	h.keys("1")
	h.mustNotHave("Ctrl-Enter Send")
}

func TestResizeReflowKeepsState(t *testing.T) {
	h := newHarness(t, demo(), 160, 40)
	h.selectByName("Create charge")
	h.keys("enter", "tab")
	sel, focus := h.a.tree.sel, h.a.focus
	for _, w := range []int{120, 90, 60, 100, 141, 200} {
		h.msg(tea.WindowSizeMsg{Width: w, Height: 30})
		if h.a.tree.sel != sel || h.a.focus != focus {
			t.Fatalf("width %d: selection/focus changed (%v, %v)", w, h.a.tree.sel, h.a.focus)
		}
		for i, line := range strings.Split(h.screen(), "\n") {
			if ui.Width(line) > w {
				t.Fatalf("width %d: line %d overflows: %q", w, i, line)
			}
		}
	}
}

func TestTooSmallTerminalExplains(t *testing.T) {
	h := newHarness(t, demo(), 30, 6)
	h.mustHave("too small")
}

func TestHeaderAlwaysShowsEnvironment(t *testing.T) {
	ws := demo()
	h := newHarness(t, ws, 80, 24)
	h.mustHave("env  none")
	h.keys("E", "down", "down", "down", "enter") // dev, local, prod
	if !strings.Contains(strings.Split(h.screen(), "\n")[0], "! prod") {
		t.Fatalf("production badge missing from header:\n%s", h.screen())
	}
	// Even a very long name keeps the environment visible.
	h.msg(tea.WindowSizeMsg{Width: 42, Height: 24})
	if !strings.Contains(strings.Split(h.screen(), "\n")[0], "prod") {
		t.Fatalf("environment dropped at 42 columns:\n%s", h.screen())
	}
}

func TestFooterHintsAreContextual(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.mustHave("n New")
	h.selectByName("Create charge")
	h.mustHave("Enter Open")
	h.selectByName("Charges")
	h.mustHave("Enter Fold")
	h.keys("/")
	h.mustHave("Esc Clear filter")
	h.mustNotHave("q Quit")
	h.keys("esc", "E")
	h.mustNotHave("n New")
	h.mustHave("Enter Use")
}

func TestDirtyMarkerFromPane(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Create charge")
	id := h.a.tree.sel
	h.msg(DirtyMsg{ID: id, Dirty: true})
	if !strings.Contains(h.screen(), "Create charge ●") {
		t.Fatalf("unsaved marker missing:\n%s", h.screen())
	}
	h.msg(DirtyMsg{ID: id, Dirty: false})
	h.mustNotHave("Create charge ●")
}

func TestSelectionMessagesReachPanes(t *testing.T) {
	rec := &recordingPane{}
	h := newHarness(t, demo(), 160, 30, func(o *Options) { o.NewResponsePane = func(PaneEnv) Pane { return rec } })
	h.selectByName("Create charge")
	if rec.last == nil || rec.last.Ref.Request == nil || rec.last.Ref.Request.Name != "Create charge" {
		t.Fatalf("selection not delivered: %+v", rec.last)
	}
	if got := strings.Join(rec.last.Ref.Path, "/"); got != "Payments/Charges/Create charge" {
		t.Fatalf("path %q", got)
	}
	if rec.last.Open {
		t.Fatal("cursor movement is not an open")
	}
	h.keys("enter")
	if !rec.last.Open || h.a.focus != SlotRequest {
		t.Fatalf("enter opens and focuses the request pane: %+v focus=%v", rec.last, h.a.focus)
	}
	if rec.reloaded == 0 || rec.envs == 0 {
		t.Fatalf("panes must learn of the workspace and environment on load (reloaded=%d envs=%d)", rec.reloaded, rec.envs)
	}
}

type recordingPane struct {
	last     *SelectionMsg
	reloaded int
	envs     int
	themes   int
}

func (p *recordingPane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	switch m := msg.(type) {
	case SelectionMsg:
		p.last = &m
	case WorkspaceReloadedMsg:
		p.reloaded++
	case EnvironmentChangedMsg:
		p.envs++
	case ThemeChangedMsg:
		p.themes++
	}
	return p, nil
}
func (p *recordingPane) View() string        { return "" }
func (p *recordingPane) SetSize(int, int)    {}
func (p *recordingPane) Focus() tea.Cmd      { return nil }
func (p *recordingPane) Blur()               {}
func (p *recordingPane) KeyHints() []ui.Hint { return nil }

func TestPanesReceiveThemeChanges(t *testing.T) {
	rec := &recordingPane{}
	h := newHarness(t, demo(), 160, 30, func(o *Options) { o.NewRequestPane = func(PaneEnv) Pane { return rec } })
	h.msg(tea.BackgroundColorMsg{})
	if rec.themes != 1 {
		t.Fatalf("themes = %d", rec.themes)
	}
}

func TestPaneCanRequestFocusToastAndReload(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.msg(FocusPaneMsg{Slot: SlotResponse})
	if h.a.focus != SlotResponse {
		t.Fatalf("focus = %v", h.a.focus)
	}
	h.msg(ToastMsg{Kind: ui.ToastSuccess, Text: "Sent"})
	h.mustHave("Sent")
	before := h.be.loads
	h.msg(ReloadMsg{})
	if h.be.loads != before+1 {
		t.Fatal("ReloadMsg must reload")
	}
}

func TestActionRegistryFeedsPalette(t *testing.T) {
	tp := &textPane{}
	h := newHarness(t, demo(), 160, 30, func(o *Options) { o.NewRequestPane = func(PaneEnv) Pane { return tp } })
	reg := h.a.registry
	ran := 0
	reg.Add(Action{ID: "x.hello", Title: "Say hello", Group: "Test", Run: func() tea.Cmd { ran++; return nil }})
	reg.Add(Action{ID: "x.hidden", Title: "Hidden thing", Enabled: func() bool { return false }, Run: func() tea.Cmd { return nil }})

	h.keys("ctrl+p")
	h.mustHave("Say hello")
	h.mustHave("Send request") // from the pane's ActionSource
	h.mustNotHave("Hidden thing")
	h.typeText("hello")
	h.keys("enter")
	if ran != 1 || h.a.ov != nil {
		t.Fatalf("action ran %d times, overlay=%v", ran, h.a.ov)
	}
	h.keys("ctrl+p")
	h.typeText("send")
	h.keys("enter")
	if len(tp.got) != 1 || tp.got[0] != "ACTION" {
		t.Fatalf("pane action did not run: %v", tp.got)
	}
}

func TestQuit(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.keys("q")
	if !h.quit {
		t.Fatal("q quits from the tree")
	}
	h = newHarness(t, demo(), 160, 30)
	h.keys("ctrl+c")
	if !h.quit {
		t.Fatal("ctrl+c quits")
	}
	h = newHarness(t, demo(), 160, 30)
	h.keys("/")
	h.typeText("q")
	if h.quit {
		t.Fatal("q types into the filter")
	}
}

func TestQuitWaitsForSaves(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.be.gate = make(chan struct{})
	h.selectByName("Create charge")
	h.a.Update(keyPress("D")) // duplicate: the save is now blocked on the gate
	if !h.a.saver.busy() {
		t.Fatal("expected a save in flight")
	}
	_, qcmd := h.a.Update(keyPress("q"))
	if msg := runQuick(qcmd); isQuit(msg) {
		t.Fatal("quit must wait for the save")
	}
	if !h.a.quitting {
		t.Fatal("quitting flag should be set")
	}
	close(h.be.gate)
}

func runQuick(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case m := <-done:
		return m
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func isQuit(m tea.Msg) bool {
	_, ok := m.(tea.QuitMsg)
	return ok
}

func TestKeyMapHasNoVimBindings(t *testing.T) {
	km := newKeyMap(false)
	for _, b := range []struct {
		name string
		keys []string
	}{{"Up", km.Up.Keys()}, {"Down", km.Down.Keys()}, {"Fold", km.Fold.Keys()}, {"Unfold", km.Unfold.Keys()}} {
		for _, k := range b.keys {
			if k == "h" || k == "j" || k == "k" || k == "l" {
				t.Errorf("%s is bound to vim key %q", b.name, k)
			}
		}
	}
}

func TestHelpIsGeneratedFromKeyMap(t *testing.T) {
	h := newHarness(t, demo(), 160, 40)
	h.keys("?")
	scr := h.screen()
	for _, want := range []string{"Everywhere", "Collections", "Request and response", "Ctrl-Enter", "Edit in $EDITOR", "Command palette", "Ctrl-P", "Duplicate", "Environments"} {
		if !strings.Contains(scr, want) {
			t.Errorf("help is missing %q:\n%s", want, scr)
		}
	}
	// Every documented binding is reachable from the key map itself.
	km := newKeyMap(false)
	for _, g := range km.helpGroups("Collections", nil) {
		if len(g.Rows) == 0 || g.Title == "" {
			t.Errorf("empty help group %+v", g)
		}
	}
	h.keys("esc")
	if h.a.ov != nil {
		t.Fatal("esc closes help")
	}
}

func TestHelpScrollsOnShortTerminals(t *testing.T) {
	h := newHarness(t, demo(), 80, 24)
	h.keys("?")
	h.mustHave("scroll for more")
	first := h.screen()
	h.keys("pgdown")
	if h.screen() == first {
		t.Fatal("help did not scroll")
	}
	h.keys("q")
	if h.a.ov != nil {
		t.Fatal("q closes help")
	}
}

func TestErrorRowsForBrokenFiles(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.be.problems = append(h.be.problems, brokenFile(h.be.root, "collections/payments/charges/create.yaml", "yaml: line 3: mapping values are not allowed here"))
	h.msg(ReloadMsg{})
	h.mustHave("Problems (1)")
	h.mustHave("×")
	h.mustHave("/create.yaml")
	h.keys("end")
	h.mustHave("Could not read create.yaml") // the request pane explains the selected problem
	h.mustHave("yaml: line 3")
	h.keys("enter")
	h.mustHave("Could not read create.yaml")
	h.mustHave("The rest of your workspace is intact")
	h.mustHave("postctl reloads it")
	if h.a.focus != SlotRequest {
		t.Fatalf("focus = %v", h.a.focus)
	}
}

func TestFatalLoadShowsErrorState(t *testing.T) {
	be := newFake(demo())
	be.loadErr = fmt.Errorf("workspace.yaml: %w", errBoom)
	h := &harness{t: t, be: be, a: newApp(Options{Backend: be, NoWatch: true})}
	h.msg(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.run(h.a.Init())
	h.mustHave("Could not open the workspace")
	h.mustHave("disk on fire")
	h.mustHave("Nothing was changed on disk")
	h.mustHave("r Try again")
	be.loadErr = nil
	h.keys("r")
	h.mustHave("Payments")
}
