package response

import (
	"testing"

	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

func TestSectionsAndStepsInTheResponsePane(t *testing.T) {
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, true, false), Actions: &tui.Registry{}}).(*Pane)
	p.SetSize(100, 30)
	if s := p.Sections(); len(s) != 1 || s[0] != tui.SectionRespBody {
		t.Fatalf("Sections = %v", s)
	}
	start := p.tab
	p.Update(tui.SectionStepMsg{Delta: 1})
	if p.tab == start {
		t.Fatal("] did not change the tab")
	}
	p.Update(tui.SectionStepMsg{Delta: -1})
	if p.tab != start {
		t.Fatal("[ did not return")
	}
	p.Update(tui.SectionStepMsg{Delta: 1})
	p.Update(tui.FocusSectionMsg{Section: tui.SectionRespBody})
	if p.tab != tabBody {
		t.Fatalf("gr -> tab %v, want Body", p.tab)
	}
	if p.VimMode() != "" {
		t.Fatalf("mode = %q while not typing", p.VimMode())
	}
}

func TestActionKeysAreRealBindings(t *testing.T) {
	own := map[string]bool{"y": true} // handled in the pane's onKey
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, true, false), Actions: &tui.Registry{}}).(*Pane)
	for _, act := range p.Actions() {
		for _, bad := range tui.UnboundActionKeys(act.ID, act.Keys) {
			if !own[bad] {
				t.Errorf("%s advertises %q, which nothing dispatches to it", act.ID, bad)
			}
		}
	}
}
