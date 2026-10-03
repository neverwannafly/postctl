package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
)

// uxPane is a request pane that records every message and reports sections,
// a vim mode and actions, so the shell's key routing can be checked.
type uxPane struct {
	textPane
	msgs     []tea.Msg
	sections []string
	mode     string
	acts     []Action
}

func (p *uxPane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	p.msgs = append(p.msgs, msg)
	if k, ok := msg.(tea.KeyPressMsg); ok {
		p.got = append(p.got, k.String())
	}
	return p, nil
}
func (p *uxPane) Sections() []string { return p.sections }
func (p *uxPane) VimMode() string    { return p.mode }
func (p *uxPane) Actions() []Action  { return p.acts }

// received returns the first message of type T the pane got.
func received[T any](p *uxPane) (T, bool) {
	for _, m := range p.msgs {
		if v, ok := m.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

func uxHarness(t *testing.T, p *uxPane, w, h int, opts ...harnessOpt) *harness {
	t.Helper()
	opts = append([]harnessOpt{func(o *Options) { o.NewRequestPane = func(PaneEnv) Pane { return p } }}, opts...)
	return newHarness(t, demo(), w, h, opts...)
}

// pop fires the which-key timer for the pending prefix, as the runtime would
// after the delay.
func (h *harness) pop() {
	h.t.Helper()
	if h.a.pend == nil {
		h.t.Fatal("no prefix is pending")
	}
	h.msg(whichKeyMsg{seq: h.a.pend.seq})
}

// ---- leader -----------------------------------------------------------------

func TestLeaderRunsTheBoundAction(t *testing.T) {
	cases := []struct {
		keys []string
		ok   func(h *harness) bool
	}{
		{[]string{"space", "p"}, func(h *harness) bool { _, ok := h.a.ov.(*pickerOverlay); return ok }},
		{[]string{"space", "e"}, func(h *harness) bool { _, ok := h.a.ov.(*envOverlay); return ok }},
		{[]string{"space", "?"}, func(h *harness) bool { _, ok := h.a.ov.(*helpOverlay); return ok }},
		{[]string{"space", "a"}, func(h *harness) bool { return h.a.ov != nil }},
		{[]string{"space", "q"}, func(h *harness) bool { return h.quit }},
	}
	for _, c := range cases {
		h := newHarness(t, demo(), 160, 30)
		h.selectByName("Create charge")
		h.keys(c.keys...)
		if !c.ok(h) {
			t.Errorf("%v did not do its action (ov=%T quit=%v)", c.keys, h.a.ov, h.quit)
		}
		if h.a.pend != nil {
			t.Errorf("%v left a prefix pending", c.keys)
		}
	}
}

func TestLeaderPaneActionsRunFromTheFocusedPane(t *testing.T) {
	var runs []string
	act := func(id string) Action {
		return Action{ID: id, Title: id, Group: "Request", Run: func() tea.Cmd { runs = append(runs, id); return nil }}
	}
	p := &uxPane{acts: []Action{act("request.curl"), act("request.curl.reveal"), act("request.editor"), act("request.reveal")}}
	h := uxHarness(t, p, 160, 30)
	h.keys("2")
	h.keys("space", "y", "space", "Y", "space", "o", "space", "v")
	if got := strings.Join(runs, " "); got != "request.curl request.curl.reveal request.editor request.reveal" {
		t.Fatalf("ran %q", got)
	}
}

// Leader keys whose action does not exist on this build are hidden from
// which-key and do nothing (find request, request history, diff, jq).
func TestLeaderKeysWithoutAnActionAreHiddenAndInert(t *testing.T) {
	h := newHarness(t, demo(), 160, 40)
	h.selectByName("Create charge")
	h.keys("space")
	h.pop()
	for _, hidden := range []string{"History of request", "Diff with previous", "jq filter"} {
		h.mustNotHave(hidden)
	}
	h.mustHave("Switch environment") // a bound key is listed
	h.mustHave("Find request")       // the request switcher (UX-Q) is bound
	h.keys("esc")
	for _, k := range [][]string{{"H"}, {"x"}, {"j"}} {
		h.keys("space")
		h.keys(k...)
		if h.a.ov != nil || h.quit || h.a.pend != nil {
			t.Fatalf("Space %v must be inert (ov=%T)", k, h.a.ov)
		}
		h.mustHave("not available here")
	}
}

// A key that a later workstream binds lights up as soon as its action exists.
func TestLeaderKeyAppearsWhenItsActionIsRegistered(t *testing.T) {
	ran := 0
	p := &uxPane{acts: []Action{{ID: "response.jq", Title: "jq", Run: func() tea.Cmd { ran++; return nil }}}}
	h := uxHarness(t, p, 160, 40)
	h.keys("2", "space")
	h.pop()
	h.mustHave("jq filter")
	h.keys("j")
	if ran != 1 {
		t.Fatalf("action ran %d times", ran)
	}
}

func TestLeaderDisabledActionIsHiddenAndInert(t *testing.T) {
	enabled := false
	ran := 0
	p := &uxPane{acts: []Action{{ID: "request.curl", Title: "Copy as curl", Enabled: func() bool { return enabled }, Run: func() tea.Cmd { ran++; return nil }}}}
	h := uxHarness(t, p, 160, 40)
	h.keys("2", "space")
	h.pop()
	h.mustNotHave("Copy as curl")
	h.keys("y")
	if ran != 0 {
		t.Fatal("a disabled action ran")
	}
	enabled = true
	h.keys("space")
	h.pop()
	h.mustHave("Copy as curl")
	h.keys("y")
	if ran != 1 {
		t.Fatal("an enabled action did not run")
	}
}

func TestLeaderUnknownKeyExplainsAndCancels(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.keys("space", "z")
	h.mustHave("No binding for Space z")
	if h.a.pend != nil || h.a.ov != nil {
		t.Fatal("an unknown follow-up key must end the sequence")
	}
}

// ---- which-key --------------------------------------------------------------

func TestWhichKeyAppearsOnlyAfterTheDelay(t *testing.T) {
	h := newHarness(t, demo(), 160, 40)
	h.keys("space")
	if h.a.pend == nil || h.a.pend.shown {
		t.Fatal("Space must arm a prefix without showing the popup yet")
	}
	h.mustNotHave("Switch environment")
	h.mustHave("Space…") // the footer already says a prefix is pending
	h.pop()
	h.mustHave("Switch environment")
	h.mustHave("Command palette")
	h.mustHave("Quit")
	if d := newApp(Options{Backend: newFake(demo())}).whichKeyDelay; d != 300*time.Millisecond {
		t.Fatalf("which-key delay = %v, want 300ms", d)
	}
}

func TestWhichKeyTimerIgnoresStalePrefixes(t *testing.T) {
	h := newHarness(t, demo(), 160, 40)
	h.keys("space")
	stale := h.a.pend.seq
	h.keys("esc", "space")
	h.msg(whichKeyMsg{seq: stale})
	if h.a.pend.shown {
		t.Fatal("a timer from a cancelled prefix opened the popup")
	}
	h.keys("e") // completes the sequence before the popup
	if h.a.pend != nil {
		t.Fatal("Space e completes the sequence")
	}
	h.msg(whichKeyMsg{seq: h.a.pendSeq})
	h.mustNotHave("Find request")
	h.mustNotHave("Command palette  ")
}

func TestWhichKeyEscClosesWithoutSideEffects(t *testing.T) {
	h := newHarness(t, demo(), 160, 40)
	h.selectByName("Create charge")
	focus, sel := h.a.focus, h.a.tree.sel
	h.keys("space")
	h.pop()
	h.keys("esc")
	if h.a.pend != nil || h.a.ov != nil || h.quit || h.a.focus != focus || h.a.tree.sel != sel {
		t.Fatal("Esc must only cancel the prefix")
	}
	h.mustNotHave("Switch environment")
	h.keys("space")
	h.pop()
	h.keys("ctrl+c")
	if h.a.pend != nil || h.quit {
		t.Fatal("Ctrl-C cancels the prefix without quitting")
	}
}

func TestWhichKeyAlwaysKeysFallThrough(t *testing.T) {
	h := newHarness(t, demo(), 160, 40)
	h.keys("space", "ctrl+p")
	if _, ok := h.a.ov.(*pickerOverlay); !ok || h.a.pend != nil {
		t.Fatalf("Ctrl-P after Space must still open the palette (ov=%T)", h.a.ov)
	}
	h.keys("esc", "space", "alt+2")
	if h.a.focus != SlotRequest || h.a.pend != nil {
		t.Fatal("Alt-2 after Space must still jump")
	}
}

func TestWhichKeyViewIsPureAndFits(t *testing.T) {
	for _, sz := range goldenSizes {
		h := newHarness(t, demo(), sz.w, sz.h)
		h.keys("space")
		h.pop()
		before := *h.a.pend
		first, second := h.screen(), h.screen()
		if first != second || *h.a.pend != before {
			t.Fatalf("%s: View changed the model", sz.name)
		}
		lines := strings.Split(first, "\n")
		if len(lines) != sz.h {
			t.Errorf("%s: %d rows, want %d", sz.name, len(lines), sz.h)
		}
		for i, l := range lines {
			if w := len([]rune(l)); w > sz.w*2 { // runes, not cells: a loose upper bound
				t.Errorf("%s: row %d is too long (%d)", sz.name, i, w)
			}
		}
	}
}

func TestGoldenWhichKey(t *testing.T) {
	looks := []struct {
		name string
		opt  harnessOpt
	}{
		{"dark", func(o *Options) {}},
		{"nocolor", func(o *Options) { o.NoColor = true }},
		{"ascii", func(o *Options) { o.ASCII = true; o.NoColor = true }},
	}
	for _, l := range looks {
		for _, sz := range goldenSizes {
			t.Run(l.name+"/"+sz.name, func(t *testing.T) {
				h := newHarness(t, demo(), sz.w, sz.h, l.opt)
				h.selectByName("Create charge")
				h.keys("space")
				h.pop()
				out := h.screen()
				golden(t, "whichkey-"+l.name+"-"+sz.name, out)
			})
		}
	}
	t.Run("goto", func(t *testing.T) {
		p := &uxPane{sections: []string{SectionURL, SectionParams, SectionHeaders, SectionBody}}
		h := uxHarness(t, p, 120, 40)
		h.keys("2", "g")
		h.pop()
		h.mustHave("Headers")
		h.mustNotHave("Tests") // not offered by this pane
		golden(t, "whichkey-goto-120x40", h.screen())
	})
}

// ---- g and bracket prefixes -------------------------------------------------

func TestGotoSectionsReachOnlyWhatThePaneOffers(t *testing.T) {
	p := &uxPane{sections: []string{SectionURL, SectionHeaders}}
	h := uxHarness(t, p, 160, 40)
	h.keys("g", "h")
	m, ok := received[FocusSectionMsg](p)
	if !ok || m.Section != SectionHeaders || h.a.focus != SlotRequest {
		t.Fatalf("gh: msg=%+v ok=%v focus=%v", m, ok, h.a.focus)
	}
	p.msgs = nil
	h.keys("1", "g", "p") // Params is not offered
	if _, ok := received[FocusSectionMsg](p); ok || h.a.focus != SlotSidebar {
		t.Fatal("gp must be inert when the pane has no Params tab")
	}
	h.mustHave("not available here")
	var offered []string
	for _, e := range h.a.gotoEntries() {
		offered = append(offered, e.Key)
	}
	if got := strings.Join(offered, " "); got != "g u h" {
		t.Fatalf("g offers %q, want only the keys the pane can reach", got)
	}
}

func TestGotoTopAndBottomInTheTree(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Create charge")
	h.keys("g", "g")
	if r, _ := h.a.tree.current(); r.label != "Payments" {
		t.Fatalf("gg -> %q", r.label)
	}
	h.keys("G")
	if r, _ := h.a.tree.current(); r.label != "Preflight" {
		t.Fatalf("G -> %q", r.label)
	}
}

func TestBracketsStepSectionsAndRepeat(t *testing.T) {
	p := &uxPane{sections: []string{SectionURL, SectionHeaders}}
	h := uxHarness(t, p, 160, 40)
	h.keys("2", "]", "]", "[")
	var deltas []int
	for _, m := range p.msgs {
		if s, ok := m.(SectionStepMsg); ok {
			deltas = append(deltas, s.Delta)
		}
	}
	if fmt.Sprint(deltas) != "[1 1 -1]" {
		t.Fatalf("section steps = %v", deltas)
	}
	h.keys("x") // any other key ends the repeat and is handled normally
	if h.a.pend != nil {
		t.Fatal("prefix still pending")
	}
}

func TestBracketsDoNothingInTheTree(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.keys("[", "]")
	if h.a.pend != nil {
		t.Fatal("[ and ] are not prefixes in the tree")
	}
}

// ---- Esc and Ctrl-C ladders ---------------------------------------------------

func TestEscLadder(t *testing.T) {
	// A text field first: Esc goes to the pane, not to the shell.
	p := &uxPane{}
	h := uxHarness(t, p, 160, 30)
	h.keys("2")
	p.editing = true
	h.keys("esc")
	if strings.Join(p.got, " ") != "esc" || h.a.focus != SlotRequest {
		t.Fatalf("pane got %v; Esc while typing belongs to the pane", p.got)
	}

	// An overlay next.
	h = newHarness(t, demo(), 160, 30)
	h.keys("ctrl+p")
	h.keys("esc")
	if h.a.ov != nil {
		t.Fatal("Esc closes the palette")
	}

	// A kept filter in the tree.
	h.keys("/")
	h.typeText("cust")
	h.keys("enter", "esc")
	if h.a.tree.query != "" {
		t.Fatal("Esc clears a kept filter")
	}

	// Otherwise it stops the running send of the selected request.
	g := &gateSender{steps: []string{"Waiting for response"}, gate: make(chan struct{})}
	sh, rec, sr := sendFixture(t, g)
	_, cmd := sh.a.Update(SendRequestMsg{Request: sr})
	flight := sh.a.sends[sr.Request.ID]
	if flight == nil {
		t.Fatal("send did not start")
	}
	// Esc goes through the shell (and its OAuth cancel) before the events are
	// delivered, so nothing touches the app from a second goroutine.
	sh.a.Update(keyPress("esc"))
	settle(t, sh, cmd)
	if len(rec.res) != 1 || !errors.Is(rec.res[0].Err, context.Canceled) {
		t.Fatalf("Esc did not cancel the send: %+v", rec.res)
	}
}

func TestCtrlCLadder(t *testing.T) {
	clock := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	newH := func(p *uxPane) *harness {
		h := uxHarness(t, p, 160, 30)
		h.a.now = func() time.Time { return clock }
		return h
	}

	// Elsewhere: once warns, twice within a second quits.
	h := newH(&uxPane{})
	h.keys("ctrl+c")
	if h.quit {
		t.Fatal("one Ctrl-C must not quit")
	}
	h.mustHave("Ctrl-C again")
	h.keys("ctrl+c")
	if !h.quit {
		t.Fatal("a second Ctrl-C quits")
	}

	// Too slow: starts over.
	h = newH(&uxPane{})
	h.keys("ctrl+c")
	clock = clock.Add(1100 * time.Millisecond)
	h.keys("ctrl+c")
	if h.quit {
		t.Fatal("Ctrl-C after more than a second only warns again")
	}

	// Another key in between resets the ladder.
	h = newH(&uxPane{})
	h.keys("ctrl+c", "down", "ctrl+c")
	if h.quit {
		t.Fatal("any other key resets the ladder")
	}

	// While typing it acts as Esc and never quits.
	p := &uxPane{}
	h = newH(p)
	h.keys("2")
	p.editing = true
	h.keys("ctrl+c", "ctrl+c", "ctrl+c")
	if h.quit {
		t.Fatal("Ctrl-C in a text field must not quit")
	}
	if strings.Join(p.got, " ") != "esc esc esc" {
		t.Fatalf("pane got %v, want Esc for each Ctrl-C", p.got)
	}

	// A pending prefix is cancelled first.
	h = newH(&uxPane{})
	h.keys("space", "ctrl+c")
	if h.a.pend != nil || h.quit {
		t.Fatal("Ctrl-C cancels a pending prefix")
	}
}

// ---- command line -----------------------------------------------------------

func TestColonCommands(t *testing.T) {
	for _, c := range []struct {
		line string
		quit bool
	}{{"q", true}, {"q!", true}, {"wq", true}, {"w", false}} {
		h := newHarness(t, demo(), 160, 30)
		h.keys(":")
		if _, ok := h.a.ov.(*pickerOverlay); !ok {
			t.Fatalf(": must open the command line (ov=%T)", h.a.ov)
		}
		h.typeText(c.line)
		h.keys("enter")
		if h.quit != c.quit {
			t.Errorf(":%s quit=%v, want %v", c.line, h.quit, c.quit)
		}
		if h.a.ov != nil {
			t.Errorf(":%s left the overlay open", c.line)
		}
	}
}

func TestColonOpensThePaletteWithTheCommandLine(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.keys(":")
	h.mustHave(":")
	h.keys("esc")
	if h.a.ov != nil {
		t.Fatal("Esc closes the command line")
	}
}

// ---- typing never triggers shortcuts ---------------------------------------

func TestTypingShortcutLettersIntoTextFields(t *testing.T) {
	// A pane text field.
	p := &uxPane{}
	h := uxHarness(t, p, 160, 30)
	h.keys("2")
	p.editing = true
	h.typeText(" gq:[]jk?") // a space arrives as the key "space"
	if h.quit || h.a.ov != nil || h.a.pend != nil || h.a.focus != SlotRequest {
		t.Fatal("typing must not trigger shortcuts")
	}
	if got := strings.Join(p.got, ""); got != "spacegq:[]jk?" {
		t.Fatalf("pane got %q", got)
	}
	h.keys("space")
	if got := p.got[len(p.got)-1]; got != "space" {
		t.Fatalf("Space reached the pane as %q", got)
	}

	// The tree filter.
	h = newHarness(t, demo(), 160, 30)
	h.keys("/")
	h.typeText("g")
	h.keys("space")
	h.typeText("q:")
	if h.quit || h.a.pend != nil || h.a.ov != nil || !h.a.tree.filtering {
		t.Fatal("the filter field must take every printable key")
	}
	if got := h.a.tree.query; got != "g q:" {
		t.Fatalf("filter text = %q", got)
	}

	// An overlay input: the palette.
	h = newHarness(t, demo(), 160, 30)
	h.keys("ctrl+p")
	h.typeText("q g:")
	h.keys("space")
	if h.quit {
		t.Fatal("palette input must not quit")
	}
	if _, ok := h.a.ov.(*pickerOverlay); !ok {
		t.Fatalf("palette closed while typing (ov=%T)", h.a.ov)
	}
}

func TestSendKeysAlwaysSend(t *testing.T) {
	ran := 0
	p := &uxPane{acts: []Action{{ID: "request.send", Title: "Send", Run: func() tea.Cmd { ran++; return nil }}}}
	h := uxHarness(t, p, 160, 30)
	h.keys("2")
	p.editing = true
	h.keys("ctrl+s")
	if got := p.got; len(got) == 0 || got[len(got)-1] != "ctrl+s" {
		t.Fatalf("Ctrl-S must reach the editing pane, got %v", got)
	}
	// From the tree, Ctrl-S runs the send action.
	p.editing = false
	h.keys("1", "ctrl+s")
	if ran != 1 {
		t.Fatalf("send action ran %d times from the tree", ran)
	}
}

// ---- pane movement ----------------------------------------------------------

func TestCtrlHJKLMovesBetweenPanesPerLayout(t *testing.T) {
	type step struct {
		key  string
		want Slot
	}
	cases := []struct {
		name  string
		width int
		steps []step
		edges []string // keys that must reach OnEdge, in order
	}{
		{"three-pane", 160,
			[]step{{"ctrl+l", SlotRequest}, {"ctrl+l", SlotResponse}, {"ctrl+l", SlotResponse}, {"ctrl+h", SlotRequest}, {"ctrl+h", SlotSidebar}, {"ctrl+h", SlotSidebar}, {"ctrl+j", SlotSidebar}, {"ctrl+k", SlotSidebar}},
			[]string{"right", "left", "down", "up"}},
		{"two-pane", 120,
			[]step{{"ctrl+l", SlotRequest}, {"ctrl+j", SlotResponse}, {"ctrl+j", SlotResponse}, {"ctrl+k", SlotRequest}, {"ctrl+h", SlotSidebar}, {"ctrl+h", SlotSidebar}},
			[]string{"down", "left"}},
		{"single-pane", 80,
			[]step{{"ctrl+l", SlotRequest}, {"ctrl+j", SlotResponse}, {"ctrl+k", SlotRequest}, {"ctrl+h", SlotSidebar}, {"ctrl+k", SlotSidebar}},
			[]string{"up"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var edges []string
			h := newHarness(t, demo(), c.width, 30, func(o *Options) {
				o.OnEdge = func(d Direction) tea.Cmd { edges = append(edges, d.String()); return nil }
			})
			h.selectByName("Create charge")
			for i, s := range c.steps {
				h.keys(s.key)
				if h.a.focus != s.want {
					t.Fatalf("step %d %s: focus %v, want %v", i, s.key, h.a.focus, s.want)
				}
			}
			if strings.Join(edges, " ") != strings.Join(c.edges, " ") {
				t.Fatalf("OnEdge got %v, want %v", edges, c.edges)
			}
		})
	}
}

func TestCtrlHJKLWithoutOnEdgeAtTheEdgeDoesNothing(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.keys("ctrl+h", "ctrl+k", "ctrl+j")
	if h.a.focus != SlotSidebar || h.a.ov != nil {
		t.Fatal("moving past the edge with no OnEdge must be a no-op")
	}
}

func TestCtrlHJKLTreatsAHiddenResponseAsTheEdge(t *testing.T) {
	ws := demo()
	var ws1 *model.Request
	for _, c := range ws.Collections {
		_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
			if r, ok := it.(*model.Request); ok && ws1 == nil {
				ws1 = r
			}
			return nil
		})
	}
	ws1.Kind = model.KindWebSocket
	var edges []string
	h := newHarness(t, ws, 160, 30, func(o *Options) {
		o.OnEdge = func(d Direction) tea.Cmd { edges = append(edges, d.String()); return nil }
	})
	h.selectByName(ws1.Name)
	h.keys("enter")
	if !h.a.responseHidden() {
		t.Fatal("a WebSocket request hides the response pane")
	}
	h.keys("ctrl+l")
	if h.a.focus != SlotRequest || strings.Join(edges, " ") != "right" {
		t.Fatalf("focus=%v edges=%v: with no response pane the request pane is the right edge", h.a.focus, edges)
	}
}

// ---- jump list --------------------------------------------------------------

func TestJumpListWalksOpenedRequests(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	name := func() string { return h.a.currentRef().Request.Name }
	for _, n := range []string{"List charges", "Create charge", "Refund charge"} {
		h.keys("1")
		h.selectByName(n)
		h.keys("enter")
	}
	h.keys("space", "[")
	if name() != "Create charge" {
		t.Fatalf("Space [ -> %q", name())
	}
	h.keys("space", "[")
	if name() != "List charges" {
		t.Fatalf("Space [ again -> %q", name())
	}
	h.keys("space", "[")
	h.mustHave("No earlier request")
	h.keys("space", "]")
	if name() != "Create charge" {
		t.Fatalf("Space ] -> %q", name())
	}
	h.keys("ctrl+o")
	if name() != "List charges" {
		t.Fatalf("Ctrl-o -> %q", name())
	}
	// Opening something new forgets the forward entries.
	h.keys("1")
	h.selectByName("Health")
	h.keys("enter", "space", "]")
	h.mustHave("No later request")
}

func TestJumpingDoesNotRecordJumps(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	for _, n := range []string{"List charges", "Create charge"} {
		h.keys("1")
		h.selectByName(n)
		h.keys("enter")
	}
	h.keys("space", "[", "space", "]", "space", "[")
	if got := len(h.a.jumps.ids); got != 2 {
		t.Fatalf("jump list has %d entries, want 2", got)
	}
}

func TestJumpListUnit(t *testing.T) {
	all := func(model.ID) bool { return true }
	var j jumpList
	if _, _, ok := j.step(-1, all); ok {
		t.Fatal("an empty list has nowhere to go")
	}
	j.push("a")
	j.push("a") // consecutive duplicates collapse
	j.push("b")
	j.push("c")
	if len(j.ids) != 3 {
		t.Fatalf("ids = %v", j.ids)
	}
	id, i, ok := j.step(-1, all)
	if !ok || id != "b" {
		t.Fatalf("back -> %q %v", id, ok)
	}
	if j.pos != 2 {
		t.Fatal("step must not move the position before commit")
	}
	j.commit(i)
	// "a" was deleted: stepping back drops it and reports nothing earlier.
	if _, _, ok := j.step(-1, func(id model.ID) bool { return id != "a" }); ok {
		t.Fatal("deleted entries must be skipped")
	}
	if len(j.ids) != 2 || j.ids[j.pos] != "b" {
		t.Fatalf("after the clean-up ids=%v pos=%d", j.ids, j.pos)
	}
	id, i, ok = j.step(1, all)
	if !ok || id != "c" {
		t.Fatalf("forward -> %q %v", id, ok)
	}
	j.commit(i)
	if _, _, ok := j.step(1, all); ok {
		t.Fatal("nothing after the newest entry")
	}
	// Pushing from the middle discards the forward entries.
	_, i, _ = j.step(-1, all)
	j.commit(i)
	j.push("d")
	if fmt.Sprint(j.ids) != "[b d]" {
		t.Fatalf("ids = %v", j.ids)
	}
}

func TestJumpListIsBounded(t *testing.T) {
	var j jumpList
	for i := 0; i < jumpDepth+50; i++ {
		j.push(model.ID(fmt.Sprintf("r%d", i)))
	}
	if len(j.ids) != jumpDepth || j.pos != jumpDepth-1 {
		t.Fatalf("len=%d pos=%d", len(j.ids), j.pos)
	}
}

// ---- badges, mode, hints ----------------------------------------------------

func TestPaneBadgesNameTheNumberKeys(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	for _, s := range []string{"[1] Collections", "[2] Request", "[3] Response"} {
		h.mustHave(s)
	}
	asc := newHarness(t, demo(), 160, 30, func(o *Options) { o.ASCII = true; o.NoColor = true })
	asc.mustHave("[1] Collections")
}

func TestFooterShowsTheVimMode(t *testing.T) {
	p := &uxPane{mode: "NORMAL"}
	h := uxHarness(t, p, 160, 30)
	h.keys("2")
	h.mustHave("NORMAL")
	p.mode = "INSERT"
	h.mustHave("INSERT")
	p.mode = ""
	h.mustNotHave("INSERT")

	h.keys("1", "/")
	h.mustHave("INSERT") // the filter is a text field
	h.keys("esc")
	h.mustNotHave("INSERT")
}

func TestFooterOffersOnlyWiredKeys(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.mustNotHave("Ctrl-E")
	h.mustNotHave("Ctrl-R")
	h.mustHave("Space Actions")
	h.keys("space")
	h.mustHave("Space…")
}

func TestViewIsPureAfterEveryPrefix(t *testing.T) {
	for _, pre := range []string{"space", "g", "ctrl+p"} {
		h := newHarness(t, demo(), 120, 40)
		h.keys("2", pre)
		if h.a.pend != nil {
			h.pop()
		}
		sel, focus, ov, pend := h.a.tree.sel, h.a.focus, h.a.ov, h.a.pend
		a, b := h.screen(), h.screen()
		if a != b || sel != h.a.tree.sel || focus != h.a.focus || ov != h.a.ov || pend != h.a.pend {
			t.Fatalf("View after %q is not pure", pre)
		}
	}
}

func TestFirstRunUsesTheLeaderForNew(t *testing.T) {
	h := newHarness(t, nil, 120, 30)
	h.mustHave("Space a")
	h.keys("space", "a")
	if h.a.ov == nil {
		t.Fatal("Space a opens the new-workspace prompt on the first-run screen")
	}
}

// ---- other prefix users -----------------------------------------------------

func TestSidebarKeepsVimMotionsAndArrows(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.keys("j", "j")
	r1, _ := h.a.tree.current()
	h.keys("down", "up", "k")
	r2, _ := h.a.tree.current()
	if r1.label == r2.label {
		t.Fatal("j/k/arrows did not move the cursor")
	}
	h.keys("G")
	if r, _ := h.a.tree.current(); r.label != "Preflight" {
		t.Fatalf("G -> %q", r.label)
	}
	h.keys("h")
	if r, _ := h.a.tree.current(); r.label != "Internal admin" {
		t.Fatalf("h on a request goes to its folder, got %q", r.label)
	}
}

func TestLeaderRenameDuplicateDeleteMove(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Create charge")
	h.keys("space", "r")
	if h.a.ov == nil {
		t.Fatal("Space r opens the rename prompt")
	}
	h.keys("esc", "space", "m")
	if h.a.ov == nil {
		t.Fatal("Space m opens the move picker")
	}
	h.keys("esc", "space", "d")
	h.mustHave("[ Cancel ]")
	h.keys("esc", "space", "D")
	h.mustHave("Create charge copy")
}

func TestLeaderKeysAreInertWhileAnOverlayIsOpen(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.keys("space", "e")
	h.keys("q")
	if h.quit {
		t.Fatal("q must not quit from inside an overlay")
	}
}
