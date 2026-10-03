package wspane

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui"
)

// HandleMouse implements tui.MouseHandler. Coordinates are relative to the
// pane's inner body (below its header).
func (p *Pane) HandleMouse(msg tea.MouseMsg, x, y int) tea.Cmd {
	if !p.editable() || p.confirm != nil {
		return nil
	}
	m := msg.Mouse()
	switch m.Button {
	case tea.MouseLeft:
		if y == 2 || y == 3 {
			tabs := p.tabList()
			hit := p.th.TabAt(p.w, tabs, x)
			if hit >= 0 && hit < len(tabSections) {
				return p.onFocusSection(tui.FocusSectionMsg{Section: tabSections[hit]})
			}
		}
	case tea.MouseWheelUp:
		return p.scrollWheel(-1)
	case tea.MouseWheelDown:
		return p.scrollWheel(1)
	}
	return nil
}

func (p *Pane) scrollWheel(delta int) tea.Cmd {
	var key rune
	if delta < 0 {
		key = tea.KeyUp
	} else {
		key = tea.KeyDown
	}
	km := tea.KeyPressMsg{Code: key}
	if p.tab == tabMessages {
		if p.detail != nil {
			return p.detailKey(km)
		}
		return p.logKey(km)
	}
	return p.bodyKey(km)
}
