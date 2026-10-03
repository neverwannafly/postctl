package response

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui"
)

// HandleMouse implements tui.MouseHandler. Coordinates are relative to the
// pane's inner body (below its header).
func (p *Pane) HandleMouse(msg tea.MouseMsg, x, y int) tea.Cmd {
	m := msg.Mouse()
	switch m.Button {
	case tea.MouseLeft:
		if y == 1 || y == 2 {
			tabs := p.buildTabs()
			hit := p.th.TabAt(p.w, tabs, x)
			if hit >= 0 && hit < len(tabs) {
				delta := hit - int(p.tab)
				_, cmd := p.Update(tui.SectionStepMsg{Delta: delta})
				return cmd
			}
		}
	case tea.MouseWheelUp:
		for i := 0; i < 3; i++ {
			p.scrollUp()
		}
	case tea.MouseWheelDown:
		for i := 0; i < 3; i++ {
			p.scrollDown()
		}
	}
	return nil
}
