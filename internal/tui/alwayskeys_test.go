package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// sendCounter is a request pane that counts the send action.
func sendCounter() (*uxPane, *int) {
	ran := new(int)
	p := &uxPane{acts: []Action{{ID: "request.send", Title: "Send", Group: "Request", Run: func() tea.Cmd { *ran++; return nil }}}}
	return p, ran
}

// overlayOpeners are the non-modal overlays and the keys that open them.
var overlayOpeners = []struct {
	name string
	open func(h *harness)
	want func(ov overlay) bool
}{
	{"palette", func(h *harness) { h.keys("ctrl+p") }, func(ov overlay) bool { _, ok := ov.(*pickerOverlay); return ok }},
	{"switcher", func(h *harness) { h.keys("space", "f") }, func(ov overlay) bool { _, ok := ov.(*switcherOverlay); return ok }},
	{"help", func(h *harness) { h.keys("?") }, func(ov overlay) bool { _, ok := ov.(*helpOverlay); return ok }},
	{"environment switcher", func(h *harness) { h.keys("space", "e") }, func(ov overlay) bool { _, ok := ov.(*envOverlay); return ok }},
	{"which-key", func(h *harness) { h.keys("space"); h.pop() }, func(ov overlay) bool { return ov == nil }},
}

// Ctrl-S, Ctrl-Enter, Alt-1/2/3 and Ctrl-P work in non-modal overlays too: the
// overlay closes, then the key acts.
func TestAlwaysKeysActInsideNonModalOverlays(t *testing.T) {
	for _, o := range overlayOpeners {
		for _, send := range []string{"ctrl+s", "ctrl+enter"} {
			t.Run(o.name+"/"+send, func(t *testing.T) {
				p, ran := sendCounter()
				h := uxHarness(t, p, 160, 30)
				o.open(h)
				if !o.want(h.a.ov) {
					t.Fatalf("%s did not open (ov=%T)", o.name, h.a.ov)
				}
				h.keys(send)
				if h.a.ov != nil || h.a.pend != nil {
					t.Fatalf("%s must close (ov=%T pend=%v)", send, h.a.ov, h.a.pend)
				}
				if *ran != 1 {
					t.Fatalf("%s sent %d times, want 1", send, *ran)
				}
			})
		}
		for key, slot := range map[string]Slot{"alt+1": SlotSidebar, "alt+2": SlotRequest, "alt+3": SlotResponse} {
			t.Run(o.name+"/"+key, func(t *testing.T) {
				p, _ := sendCounter()
				h := uxHarness(t, p, 160, 30)
				h.keys("2") // so Alt-1 and Alt-3 really move focus
				o.open(h)
				h.keys(key)
				if h.a.ov != nil || h.a.pend != nil || h.a.focus != slot {
					t.Fatalf("%s: ov=%T pend=%v focus=%v, want closed and focus %v", key, h.a.ov, h.a.pend, h.a.focus, slot)
				}
			})
		}
	}
}

func TestCtrlPInsideOverlays(t *testing.T) {
	type outcome func(ov overlay) bool
	isPicker := func(ov overlay) bool { _, ok := ov.(*pickerOverlay); return ok }
	cases := map[string]outcome{
		"palette":              func(ov overlay) bool { return ov == nil }, // toggles closed
		"switcher":             func(ov overlay) bool { _, ok := ov.(*switcherOverlay); return ok },
		"help":                 isPicker,
		"environment switcher": isPicker,
		"which-key":            isPicker,
	}
	for _, o := range overlayOpeners {
		t.Run(o.name, func(t *testing.T) {
			p, _ := sendCounter()
			h := uxHarness(t, p, 160, 30)
			o.open(h)
			h.keys("ctrl+p")
			if !cases[o.name](h.a.ov) {
				t.Fatalf("ctrl+p in %s left ov=%T", o.name, h.a.ov)
			}
		})
	}
}

// Esc and Ctrl-C keep their meaning in every overlay.
func TestEscAndCtrlCStillLeaveOverlays(t *testing.T) {
	for _, o := range overlayOpeners {
		t.Run(o.name+"/esc", func(t *testing.T) {
			p, ran := sendCounter()
			h := uxHarness(t, p, 160, 30)
			o.open(h)
			h.keys("esc")
			if h.a.ov != nil || h.a.pend != nil || *ran != 0 {
				t.Fatalf("esc: ov=%T pend=%v sends=%d", h.a.ov, h.a.pend, *ran)
			}
		})
	}
	// Ctrl-C keeps its ladder: it cancels a pending prefix, then needs a
	// second press to quit, whatever overlay is open.
	p, _ := sendCounter()
	h := uxHarness(t, p, 160, 30)
	h.keys("space", "ctrl+c")
	if h.a.pend != nil || h.quit {
		t.Fatalf("Ctrl-C cancels the prefix only (pend=%v quit=%v)", h.a.pend, h.quit)
	}
	h.keys("?", "ctrl+c")
	if h.quit {
		t.Fatal("one Ctrl-C must not quit")
	}
	h.keys("ctrl+c")
	if !h.quit {
		t.Fatal("a second Ctrl-C quits")
	}
}

// The switcher keeps Esc, Ctrl-n and Ctrl-p under the shell's routing.
func TestSwitcherKeepsItsOwnKeys(t *testing.T) {
	p, ran := sendCounter()
	h := uxHarness(t, p, 160, 30)
	h.keys("space", "f")
	sw, ok := h.a.ov.(*switcherOverlay)
	if !ok || len(sw.res) < 3 {
		t.Fatalf("switcher not open with results (ov=%T)", h.a.ov)
	}
	h.keys("ctrl+n", "ctrl+n")
	if sw.cursor != 2 {
		t.Fatalf("ctrl+n twice -> cursor %d, want 2", sw.cursor)
	}
	h.keys("ctrl+p")
	if h.a.ov != sw || sw.cursor != 1 {
		t.Fatalf("ctrl+p moves up inside the switcher (ov=%T cursor=%d)", h.a.ov, sw.cursor)
	}
	h.keys("esc")
	if h.a.ov != nil || *ran != 0 {
		t.Fatalf("esc closes the switcher (ov=%T)", h.a.ov)
	}
	h.keys("space", "space") // second binding for the same action
	if _, ok := h.a.ov.(*switcherOverlay); !ok {
		t.Fatalf("Space Space opens the switcher (ov=%T)", h.a.ov)
	}
}

// Confirmation dialogs and prompts must be answered first.
func TestModalDialogsIgnoreAlwaysKeys(t *testing.T) {
	for name, open := range map[string][]string{"confirm": {"space", "d"}, "prompt": {"space", "r"}} {
		for _, key := range []string{"ctrl+s", "ctrl+enter", "alt+2", "alt+3", "ctrl+p"} {
			t.Run(name+"/"+key, func(t *testing.T) {
				p, ran := sendCounter()
				h := uxHarness(t, p, 160, 30)
				h.selectByName("Create charge")
				h.keys(open...)
				before := h.a.ov
				if before == nil {
					t.Fatal("dialog did not open")
				}
				focus := h.a.focus
				h.keys(key)
				if h.a.ov != before || h.a.focus != focus || *ran != 0 {
					t.Fatalf("%s must be ignored by the %s dialog (ov=%T focus=%v sends=%d)", key, name, h.a.ov, h.a.focus, *ran)
				}
			})
		}
	}
}

// Full-screen screens keep their own keys; only Ctrl-P closes them.
func TestScreensKeepTheirKeysAndCtrlPClosesThem(t *testing.T) {
	p, ran := sendCounter()
	s := &fakeScreen{}
	h := newHarness(t, demo(), 120, 30, func(o *Options) {
		o.NewEnvEditor = func(env ScreenEnv) Screen { s.env = env; return s }
		o.NewRequestPane = func(PaneEnv) Pane { return p }
	})
	h.keys("space", "E")
	h.mustHave("FAKE SCREEN")
	h.keys("ctrl+s", "alt+2")
	got := " " + strings.Join(s.keys, " ") + " "
	for _, want := range []string{"ctrl+s", "alt+2"} {
		if !strings.Contains(got, " "+want+" ") {
			t.Errorf("screen did not get %s (keys %q)", want, got)
		}
	}
	if *ran != 0 || h.a.focus == SlotRequest {
		t.Fatalf("the screen keeps its keys: sends=%d focus=%v", *ran, h.a.focus)
	}
	h.keys("ctrl+p")
	h.mustNotHave("FAKE SCREEN")
}

// ---- contextual / ---------------------------------------------------------

func TestSlashIsContextual(t *testing.T) {
	req, resp := &uxPane{}, &uxPane{}
	h := newHarness(t, demo(), 160, 30, func(o *Options) {
		o.NewRequestPane = func(PaneEnv) Pane { return req }
		o.NewResponsePane = func(PaneEnv) Pane { return resp }
	})
	h.keys("/")
	if h.a.focus != SlotSidebar || !h.a.tree.filtering {
		t.Fatalf("/ with Collections focused filters the tree (filtering=%v)", h.a.tree.filtering)
	}
	h.keys("esc")
	for _, c := range []struct {
		slot string
		pane *uxPane
	}{{"2", req}, {"3", resp}} {
		h.keys(c.slot)
		before := len(c.pane.got)
		h.keys("/")
		if h.a.tree.filtering || h.a.focus == SlotSidebar {
			t.Fatalf("/ in pane %s must not open the tree filter (focus=%v)", c.slot, h.a.focus)
		}
		if got := c.pane.got[before:]; len(got) != 1 || got[0] != "/" {
			t.Fatalf("pane %s should decide what / does, got %v", c.slot, got)
		}
	}
}

// ---- swallowed key after a prefix ------------------------------------------

func TestUnknownKeyAfterAPrefixSaysSo(t *testing.T) {
	cases := []struct {
		keys []string
		want string
	}{
		{[]string{"[", "x"}, "No binding for [x"},
		{[]string{"]", "z"}, "No binding for ]z"},
		{[]string{"g", "z"}, "No binding for gz"},
		{[]string{"space", "z"}, "No binding for Space z"},
		{[]string{"]", "]", "x"}, "No binding for ]x"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.keys, ""), func(t *testing.T) {
			p := &uxPane{sections: []string{"A", "B", "C"}}
			h := uxHarness(t, p, 160, 30)
			h.keys("2")
			h.keys(c.keys...)
			h.mustHave(c.want)
			if h.a.pend != nil {
				t.Fatal("the prefix must be cancelled")
			}
			for _, k := range p.got {
				if k == "x" || k == "z" {
					t.Fatalf("the unknown key %q leaked to the pane", k)
				}
			}
		})
	}
}

// ---- Esc cancels OAuth from any pane --------------------------------------

func TestEscCancelsOAuthFromAnyPane(t *testing.T) {
	for _, slot := range []string{"1", "3"} {
		t.Run("pane "+slot, func(t *testing.T) {
			p := &uxPane{}
			h := uxHarness(t, p, 160, 30)
			h.keys(slot)
			if _, ok := received[CancelOAuthMsg](p); ok {
				t.Fatal("cancel sent too early")
			}
			h.keys("esc")
			if _, ok := received[CancelOAuthMsg](p); !ok {
				t.Fatalf("Esc in pane %s did not reach the request pane as a cancel", slot)
			}
		})
	}
	// While typing, Esc only leaves the text field.
	p := &uxPane{}
	h := uxHarness(t, p, 160, 30)
	h.keys("/") // tree filter active
	h.keys("esc")
	if _, ok := received[CancelOAuthMsg](p); ok {
		t.Fatal("Esc while typing in the filter must not cancel a sign-in")
	}
}

// ---- pure View with a pending prefix --------------------------------------

func TestShellViewIsPureWithPrefixAndWhichKey(t *testing.T) {
	h := newHarness(t, demo(), 120, 30)
	state := func() string {
		return fmt.Sprintf("%+v|%d|%d|%T", *h.a.pend, h.a.tree.top, len(h.a.tree.cache), h.a.ov)
	}
	h.keys("space") // armed, before 300 ms
	if h.a.pend == nil || h.a.pend.shown {
		t.Fatal("prefix must be armed and hidden")
	}
	before := state()
	first, second := h.a.View().Content, h.a.View().Content
	if first != second || before != state() {
		t.Fatal("View mutated the armed-prefix state")
	}
	h.pop() // which-key popup open
	if !h.a.pend.shown {
		t.Fatal("which-key must be showing")
	}
	before = state()
	first, second = h.a.View().Content, h.a.View().Content
	if first != second || before != state() {
		t.Fatal("View mutated the which-key state")
	}
}
