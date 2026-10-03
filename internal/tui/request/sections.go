package request

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui"
)

// tabKeys are the letters after g that jump to each tab (gp, ga, gh, gb, gs,
// gt, gS), in tab order.
var tabKeys = [numTabs]string{"p", "a", "h", "b", "s", "t", "S"}

// tabSections are the section names (tui.Section constants) of the tabs, in
// tab order.
var tabSections = [numTabs]string{
	tui.SectionParams, tui.SectionAuth, tui.SectionHeaders, tui.SectionBody,
	tui.SectionPre, tui.SectionTests, tui.SectionSettings,
}

// Sections implements tui.SectionLister: the URL bar and the seven tabs while
// an HTTP request is open.
func (p *Pane) Sections() []string {
	if !p.editable() {
		return nil
	}
	return append([]string{tui.SectionURL}, tabSections[:]...)
}

// VimMode implements tui.ModeReporter: the mode of the focused field (the URL
// bar or a field of the open tab), or "" when no field has the keyboard. The
// shell reads it as the typing gate: NORMAL does not block the leader.
func (p *Pane) VimMode() string {
	if f := p.activeField(); f != nil {
		return f.ModeName()
	}
	return ""
}

// onFocusSection shows a section and puts the cursor in it (g + letter).
func (p *Pane) onFocusSection(m tui.FocusSectionMsg) tea.Cmd {
	if !p.editable() || p.confirm != nil {
		return nil
	}
	if m.Section == tui.SectionURL {
		p.stopViews()
		p.setZone(zBar)
		return nil
	}
	for i, name := range tabSections {
		if name != m.Section {
			continue
		}
		p.stopViews()
		p.tab = tabID(i)
		p.setZone(zBody)
		p.views[i].enter(p)
	}
	return nil
}

// typingNow reports whether keys are text right now: a field in insert or
// visual mode, or a dialog. A field in normal mode is not typing, so the
// shell's section keys reach it.
func (p *Pane) typingNow() bool {
	if !p.Editing() {
		return false
	}
	f := p.activeField()
	return f == nil || f.Typing()
}

// onSectionStep moves to the previous or next tab ([ and ]) unless a text
// field is being typed in.
func (p *Pane) onSectionStep(m tui.SectionStepMsg) tea.Cmd {
	if !p.editable() || p.typingNow() || m.Delta == 0 {
		return nil
	}
	p.stopViews()
	p.tab = tabID((int(p.tab) + m.Delta + int(numTabs)) % int(numTabs))
	if p.zone == zBody {
		p.views[p.tab].enter(p)
	}
	return nil
}

// stopViews leaves every tab's edit mode and closes the undo session of the
// field being left.
func (p *Pane) stopViews() {
	p.endField()
	for _, v := range p.views {
		v.stop()
	}
}
