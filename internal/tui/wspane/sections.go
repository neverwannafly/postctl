package wspane

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui"
)

// tabKeys are the letters after g that jump to each tab (gm, gp, gh, ga, gS),
// in tab order.
var tabKeys = [numTabs]string{"m", "p", "h", "a", "S"}

// tabSections are the section names (tui.Section constants) of the tabs, in
// tab order.
var tabSections = [numTabs]string{
	tui.SectionMessages, tui.SectionParams, tui.SectionHeaders, tui.SectionAuth, tui.SectionSettings,
}

// Sections implements tui.SectionLister: the URL bar and the five tabs while a
// WebSocket request is open.
func (p *Pane) Sections() []string {
	if !p.editable() {
		return nil
	}
	return append([]string{tui.SectionURL}, tabSections[:]...)
}

// VimMode implements tui.ModeReporter: the mode of the focused field (the URL
// bar, the composer or a cell of the open tab), or "" when no field has the
// keyboard. The shell reads it as the typing gate: NORMAL does not block the
// leader.
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
		p.stopEditing()
		p.setZone(zBar)
		return nil
	}
	for i, name := range tabSections {
		if name != m.Section {
			continue
		}
		p.stopEditing()
		p.tab = tabID(i)
		p.enterBody()
	}
	return nil
}

// typingNow reports whether keys are text right now: a field in insert or
// visual mode, or a prompt or dialog. A field in normal mode is not typing, so
// the shell's section keys reach it.
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
	p.stopEditing()
	p.tab = tabID((int(p.tab) + m.Delta + int(numTabs)) % int(numTabs))
	if p.zone == zBody {
		p.enterBody()
	}
	return nil
}
