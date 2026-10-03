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

// VimMode implements tui.ModeReporter. The text fields are not modal yet, so
// editing one reports INSERT.
func (p *Pane) VimMode() string {
	if p.Editing() {
		return "INSERT"
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

// onSectionStep moves to the previous or next tab ([ and ]) unless a text
// field has the keyboard.
func (p *Pane) onSectionStep(m tui.SectionStepMsg) tea.Cmd {
	if !p.editable() || p.Editing() || m.Delta == 0 {
		return nil
	}
	p.stopEditing()
	p.tab = tabID((int(p.tab) + m.Delta + int(numTabs)) % int(numTabs))
	if p.zone == zBody {
		p.enterBody()
	}
	return nil
}
