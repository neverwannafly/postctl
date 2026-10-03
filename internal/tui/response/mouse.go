package response

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

const (
	responseSummaryRows = 1
	responseTabsRows    = 2
	responseChromeRows  = responseSummaryRows + responseTabsRows
)

// HandleMouse implements tui.MouseHandler. Coordinates are relative to the
// pane's inner body (below its header).
func (p *Pane) HandleMouse(msg tea.MouseMsg, x, y int) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return nil
		}
		p.mouse.Release()
		if p.diff.open {
			return nil
		}
		if v := p.readyVim(); v != nil && p.vimOnScreen() {
			originY := responseChromeRows
			if st := p.current(); st != nil {
				originY += len(st.result.Warnings)
				if st.result.Response != nil && st.result.Response.BodyTruncated {
					originY++
				}
			}
			if y >= originY && y < originY+v.h {
				if p.mouse.Press(v.f, x, y-originY, p.deps.now()) {
					p.mouseX, p.mouseY = 0, originY
				}
				return nil
			}
		}
		w := max(10, p.w)
		tabs := p.buildTabs()
		t := p.th
		bar := t.Tabs(w, tabs, p.tabIndex(), p.focused)
		collapsed := true
		if ui.Width(bar) > 0 && strings.Count(bar, "\n") >= 1 {
			first := strings.SplitN(bar, "\n", 2)[0]
			if ui.Width(first)+10 < w {
				collapsed = false
			}
		}

		if collapsed {
			if y == responseSummaryRows {
				label := tabs[p.tabIndex()].Label
				selText := fmt.Sprintf("[%d/%d] %s  ] next", p.tabIndex()+1, len(tabs), label)
				if x >= 0 && x < ui.Width(selText) {
					_, cmd := p.Update(tui.SectionStepMsg{Delta: 1})
					return cmd
				}
			}
			return nil
		}

		if y >= responseSummaryRows && y < responseChromeRows {
			hit := p.th.TabAt(w, tabs, x)
			if hit >= 0 && hit < len(tabs) {
				delta := hit - p.tabIndex()
				if delta != 0 {
					_, cmd := p.Update(tui.SectionStepMsg{Delta: delta})
					return cmd
				}
			}
		}
	case tea.MouseMotionMsg:
		if msg.Button == tea.MouseLeft && p.mouse.Active() {
			p.mouse.Motion(x-p.mouseX, y-p.mouseY)
		}
	case tea.MouseReleaseMsg:
		p.mouse.Release()
	case tea.MouseWheelMsg:
		if p.mouse.Active() {
			return nil
		}
		if p.diff.open {
			switch msg.Button {
			case tea.MouseWheelUp:
				return p.diffKey("ctrl+u", tea.KeyPressMsg{})
			case tea.MouseWheelDown:
				return p.diffKey("ctrl+d", tea.KeyPressMsg{})
			}
			return nil
		}
		if p.tab == tabHistory && p.historyEnabled() {
			switch msg.Button {
			case tea.MouseWheelUp:
				p.histMove(-3)
			case tea.MouseWheelDown:
				p.histMove(3)
			}
			return nil
		}
		if p.vimLive() {
			switch msg.Button {
			case tea.MouseWheelUp:
				p.vimWheel(-3)
			case tea.MouseWheelDown:
				p.vimWheel(3)
			}
			return nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			for i := 0; i < 3; i++ {
				p.scrollUp()
			}
		case tea.MouseWheelDown:
			for i := 0; i < 3; i++ {
				p.scrollDown()
			}
		}
	}
	return nil
}
