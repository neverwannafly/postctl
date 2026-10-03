package request

import (
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/tui"
)

func TestSectionsListEveryTabAndTheURL(t *testing.T) {
	h := newHarness(t)
	got := strings.Join(h.p.Sections(), " ")
	for _, want := range []string{tui.SectionURL, tui.SectionParams, tui.SectionAuth, tui.SectionHeaders, tui.SectionBody, tui.SectionPre, tui.SectionTests, tui.SectionSettings} {
		if !strings.Contains(got, want) {
			t.Errorf("Sections lacks %q: %s", want, got)
		}
	}
}

func TestFocusSectionShowsTheTab(t *testing.T) {
	cases := map[string]tabID{
		tui.SectionParams: tabParams, tui.SectionAuth: tabAuth, tui.SectionHeaders: tabHeaders,
		tui.SectionBody: tabBody, tui.SectionPre: tabPre, tui.SectionTests: tabTests, tui.SectionSettings: tabSettings,
	}
	for section, want := range cases {
		h := newHarness(t)
		h.send(tui.FocusSectionMsg{Section: section})
		if h.p.tab != want {
			t.Errorf("%s: tab = %v, want %v", section, h.p.tab, want)
		}
	}
}

func TestFocusURLSectionMovesToTheBar(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabHeaders)
	h.send(tui.FocusSectionMsg{Section: tui.SectionURL})
	if h.p.zone != zBar {
		t.Fatalf("zone = %v, want the URL bar", h.p.zone)
	}
}

func TestSectionStepWrapsAndIsIgnoredWhileTyping(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabSettings)
	h.send(tui.SectionStepMsg{Delta: 1})
	if h.p.tab != tabParams {
		t.Fatalf("] after the last tab -> %v", h.p.tab)
	}
	h.send(tui.SectionStepMsg{Delta: -1})
	if h.p.tab != tabSettings {
		t.Fatalf("[ before the first tab -> %v", h.p.tab)
	}
	h.send(tui.FocusSectionMsg{Section: tui.SectionURL})
	h.key("a") // typing in the URL bar
	if !h.p.Editing() {
		t.Fatal("the URL bar should be editing")
	}
	before := h.p.tab
	h.send(tui.SectionStepMsg{Delta: 1})
	if h.p.tab != before {
		t.Fatal("] while typing must not change the tab")
	}
	if h.p.VimMode() != "INSERT" {
		t.Fatalf("mode = %q while typing", h.p.VimMode())
	}
}

func TestUndoRedoKeysWhenNotTyping(t *testing.T) {
	h := newHarness(t)
	h.key("down", "d") // delete the first query row
	if len(h.req.URL.Query) != 1 {
		t.Fatalf("query = %v", h.req.URL.Query)
	}
	h.key("u")
	if len(h.req.URL.Query) != 2 {
		t.Fatalf("u did not undo: %v", h.req.URL.Query)
	}
	h.key("ctrl+r")
	if len(h.req.URL.Query) != 1 {
		t.Fatalf("Ctrl-r did not redo: %v", h.req.URL.Query)
	}
}

func TestHiddenLetterShortcutsAreGone(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabHeaders)
	for _, k := range []string{"e", "y", "Y", "ctrl+o", "ctrl+l"} {
		h.key(k)
		if len(h.clip) != 0 || h.p.zone == zBar {
			t.Fatalf("%s must no longer act inside the pane (moved to the leader)", k)
		}
	}
}
