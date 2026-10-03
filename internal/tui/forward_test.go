package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyOf(s string) tea.KeyPressMsg {
	if s == "space" {
		return tea.KeyPressMsg(tea.Key{Code: ' ', Text: " "})
	}
	r := []rune(s)
	return tea.KeyPressMsg(tea.Key{Code: r[0], Text: s})
}

// While a modal field is in normal mode the shell lets its keys through to
// the global layer; in insert and visual mode every key belongs to the field.
func TestTypingGateIsModeAware(t *testing.T) {
	cases := []struct {
		mode  string
		typed bool
	}{
		{"NORMAL", false}, {"INSERT", true}, {"VISUAL", true}, {"VISUAL LINE", true}, {"", true},
	}
	for _, c := range cases {
		p := &uxPane{mode: c.mode}
		h := uxHarness(t, p, 160, 30)
		h.keys("2")
		p.editing = true
		h.a.pend = nil
		if got := h.a.typing(); got != c.typed {
			t.Errorf("mode %q: typing = %v, want %v", c.mode, got, c.typed)
		}
	}
}

// ForwardKeyMsg runs the shell's own handling of the key: Space opens the
// leader, ":" the command line, "?" the help; and a forwarded key never goes
// back to the pane.
func TestForwardedKeysReachTheShellNotThePane(t *testing.T) {
	p := &uxPane{mode: "NORMAL"}
	h := uxHarness(t, p, 160, 30)
	h.keys("2")
	p.editing = true
	before := len(p.got)

	h.msg(ForwardKeyMsg{Key: keyOf("space")})
	if h.a.pend == nil || h.a.pend.prefix != prefixLeader {
		t.Fatal("a forwarded Space should open the leader")
	}
	h.keys("esc")

	h.msg(ForwardKeyMsg{Key: keyOf("?")})
	if _, ok := h.a.ov.(*helpOverlay); !ok {
		t.Fatalf("a forwarded ? should open help, got %T", h.a.ov)
	}
	h.keys("esc")

	h.msg(ForwardKeyMsg{Key: keyOf(":")})
	if h.a.ov == nil {
		t.Fatal("a forwarded : should open the command line")
	}
	h.keys("esc")

	h.msg(ForwardKeyMsg{Key: keyOf("q")})
	if !h.quit {
		t.Fatal("a forwarded q should quit")
	}
	for _, k := range p.got[before:] {
		if k == " " || k == "space" || k == ":" || k == "?" || k == "q" {
			t.Fatalf("a forwarded key came back to the pane: %v", p.got[before:])
		}
	}
}

func TestForwardedSequenceRunsTheGotoKey(t *testing.T) {
	p := &uxPane{mode: "NORMAL", sections: []string{SectionURL, SectionParams, SectionBody}}
	h := uxHarness(t, p, 160, 30)
	h.keys("2")
	p.editing = true
	h.msg(ForwardSeqMsg{Seq: "gb"})
	fs, ok := received[FocusSectionMsg](p)
	if !ok || fs.Section != SectionBody {
		t.Fatalf("gb should focus the Body section, got %+v ok=%v", fs, ok)
	}
	// Anything else is ignored.
	n := len(p.msgs)
	h.msg(ForwardSeqMsg{Seq: "gg"})
	h.msg(ForwardSeqMsg{Seq: "zz"})
	h.msg(ForwardSeqMsg{Seq: "g"})
	if len(p.msgs) != n || h.quit || h.a.ov != nil {
		t.Fatal("malformed sequences must be ignored")
	}
}

// A forwarded key is dropped while an overlay is open or a prefix is pending,
// so a late message cannot act under a dialog.
func TestForwardedKeysAreDroppedUnderOverlays(t *testing.T) {
	p := &uxPane{mode: "NORMAL"}
	h := uxHarness(t, p, 160, 30)
	h.keys("2")
	p.editing = true
	h.keys("ctrl+p")
	ov := h.a.ov
	h.msg(ForwardKeyMsg{Key: keyOf("q")})
	if h.quit || h.a.ov != ov {
		t.Fatal("a forwarded key acted under an overlay")
	}
}

// With a field in normal mode the footer drops the typing-only hint set.
func TestFooterInNormalModeOffersTheLeader(t *testing.T) {
	p := &uxPane{mode: "NORMAL"}
	h := uxHarness(t, p, 160, 30)
	h.keys("2")
	p.editing = true
	h.mustHave("Space Actions")
	p.mode = "INSERT"
	h.mustNotHave("Space Actions")
}
